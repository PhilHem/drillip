package main

import (
	"compress/gzip"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var db *sql.DB

// Sentry event types

type Event struct {
	EventID     string            `json:"event_id"`
	Exception   *ExceptionData    `json:"exception"`
	Breadcrumbs *BreadcrumbData   `json:"breadcrumbs"`
	Release     string            `json:"release"`
	Environment string            `json:"environment"`
	User        json.RawMessage   `json:"user"`
	Tags        map[string]string `json:"tags"`
	Platform    string            `json:"platform"`
	ServerName  string            `json:"server_name"`
}

type ExceptionData struct {
	Values []ExceptionValue `json:"values"`
}

type ExceptionValue struct {
	Type       string      `json:"type"`
	Value      string      `json:"value"`
	Stacktrace *Stacktrace `json:"stacktrace"`
}

type Stacktrace struct {
	Frames []Frame `json:"frames"`
}

type Frame struct {
	Filename string `json:"filename"`
	Function string `json:"function"`
	Lineno   int    `json:"lineno"`
	Colno    int    `json:"colno"`
	AbsPath  string `json:"abs_path"`
	Module   string `json:"module"`
}

type BreadcrumbData struct {
	Values []json.RawMessage `json:"values"`
}

type EnvelopeItemHeader struct {
	Type   string `json:"type"`
	Length int    `json:"length"`
}

func initDB(path string) error {
	var err error
	db, err = sql.Open("sqlite", path)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
		PRAGMA journal_mode=WAL;
		PRAGMA synchronous=NORMAL;
		PRAGMA busy_timeout=5000;

		CREATE TABLE IF NOT EXISTS errors (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			fingerprint  TEXT UNIQUE,
			type         TEXT,
			value        TEXT,
			stacktrace   TEXT,
			breadcrumbs  TEXT,
			release_tag  TEXT,
			environment  TEXT,
			user_context TEXT,
			tags         TEXT,
			platform     TEXT,
			first_seen   TEXT,
			last_seen    TEXT,
			count        INTEGER DEFAULT 1
		);

		CREATE INDEX IF NOT EXISTS idx_errors_last_seen ON errors(last_seen);
		CREATE INDEX IF NOT EXISTS idx_errors_type ON errors(type);
		CREATE INDEX IF NOT EXISTS idx_errors_release ON errors(release_tag);
		CREATE INDEX IF NOT EXISTS idx_errors_count ON errors(count);
	`)
	return err
}

func fingerprint(ev *Event) string {
	h := sha256.New()
	if ev.Exception != nil && len(ev.Exception.Values) > 0 {
		exc := ev.Exception.Values[0]
		h.Write([]byte(exc.Type))
		if exc.Stacktrace != nil && len(exc.Stacktrace.Frames) > 0 {
			f := exc.Stacktrace.Frames[len(exc.Stacktrace.Frames)-1]
			h.Write([]byte(f.Filename))
			h.Write([]byte(f.Function))
			h.Write([]byte(fmt.Sprintf("%d", f.Lineno)))
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

func parseEnvelope(body []byte) (*Event, error) {
	lines := strings.Split(string(body), "\n")

	// First line: envelope header (skip)
	// Subsequent pairs: item header + item payload
	for i := 1; i+1 < len(lines); i += 2 {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		var hdr EnvelopeItemHeader
		if err := json.Unmarshal([]byte(lines[i]), &hdr); err != nil {
			continue
		}
		if hdr.Type == "event" {
			var ev Event
			if err := json.Unmarshal([]byte(lines[i+1]), &ev); err != nil {
				return nil, fmt.Errorf("parse event: %w", err)
			}
			return &ev, nil
		}
	}
	return nil, fmt.Errorf("no event item in envelope")
}

func readBody(r *http.Request) ([]byte, error) {
	var reader io.Reader = r.Body
	if r.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(r.Body)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		reader = gz
	}
	return io.ReadAll(reader)
}

func storeError(ev *Event) error {
	fp := fingerprint(ev)
	now := time.Now().UTC().Format(time.RFC3339)

	var excType, excValue, stacktraceJSON string
	if ev.Exception != nil && len(ev.Exception.Values) > 0 {
		exc := ev.Exception.Values[0]
		excType = exc.Type
		excValue = exc.Value
		if b, err := json.Marshal(exc.Stacktrace); err == nil {
			stacktraceJSON = string(b)
		}
	}

	var breadcrumbsJSON string
	if ev.Breadcrumbs != nil {
		if b, err := json.Marshal(ev.Breadcrumbs.Values); err == nil {
			breadcrumbsJSON = string(b)
		}
	}

	userJSON := string(ev.User)

	var tagsJSON string
	if ev.Tags != nil {
		if b, err := json.Marshal(ev.Tags); err == nil {
			tagsJSON = string(b)
		}
	}

	_, err := db.Exec(`
		INSERT INTO errors (fingerprint, type, value, stacktrace, breadcrumbs,
			release_tag, environment, user_context, tags, platform, first_seen, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(fingerprint) DO UPDATE SET
			last_seen = ?,
			count = count + 1,
			breadcrumbs = ?,
			user_context = ?,
			release_tag = COALESCE(?, release_tag)
	`, fp, excType, excValue, stacktraceJSON, breadcrumbsJSON,
		ev.Release, ev.Environment, userJSON, tagsJSON, ev.Platform, now, now,
		now, breadcrumbsJSON, userJSON, ev.Release)
	return err
}

func handleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := readBody(r)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Try envelope format first, fall back to plain JSON
	event, err := parseEnvelope(body)
	if err != nil {
		var ev Event
		if jsonErr := json.Unmarshal(body, &ev); jsonErr != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		event = &ev
	}

	if event.Exception == nil || len(event.Exception.Values) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"ok"}`))
		return
	}

	if err := storeError(event); err != nil {
		log.Printf("store error: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"id":"ok"}`))
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := db.Ping(); err != nil {
		http.Error(w, "db unhealthy", http.StatusServiceUnavailable)
		return
	}
	w.Write([]byte("ok"))
}

func main() {
	dbPath := "errors.db"
	addr := "127.0.0.1:8300"

	if v := os.Getenv("ERROR_SINK_DB"); v != "" {
		dbPath = v
	}
	if v := os.Getenv("ERROR_SINK_ADDR"); v != "" {
		addr = v
	}

	if err := initDB(dbPath); err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer db.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/", handleIngest)
	mux.HandleFunc("/-/healthy", handleHealth)

	log.Printf("error-sink listening on %s (db: %s)", addr, dbPath)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
