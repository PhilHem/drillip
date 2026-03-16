package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/andybalholm/brotli"
)

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
	switch r.Header.Get("Content-Encoding") {
	case "gzip":
		gz, err := gzip.NewReader(r.Body)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		reader = gz
	case "br":
		reader = brotli.NewReader(r.Body)
	}
	return io.ReadAll(reader)
}

func storeEvent(ev *Event) (string, error) {
	fp := fingerprint(ev)
	now := time.Now().UTC().Format(time.RFC3339)
	level := ev.EffectiveLevel()

	var evType, evValue, stacktraceJSON string

	if ev.Exception != nil && len(ev.Exception.Values) > 0 {
		exc := ev.Exception.Values[0]
		evType = exc.Type
		evValue = exc.Value
		if b, err := json.Marshal(exc.Stacktrace); err == nil {
			stacktraceJSON = string(b)
		}
	} else {
		evType = "message"
		evValue = ev.messageText()
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

	tx, err := db.Begin()
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.Exec(`
		INSERT INTO errors (fingerprint, type, value, level, stacktrace, breadcrumbs,
			release_tag, environment, user_context, tags, platform, first_seen, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(fingerprint) DO UPDATE SET
			last_seen = ?,
			count = count + 1,
			breadcrumbs = ?,
			user_context = ?,
			release_tag = COALESCE(?, release_tag)
	`, fp, evType, evValue, level, stacktraceJSON, breadcrumbsJSON,
		ev.Release, ev.Environment, userJSON, tagsJSON, ev.Platform, now, now,
		now, breadcrumbsJSON, userJSON, ev.Release)
	if err != nil {
		return "", err
	}

	_, err = tx.Exec(`INSERT INTO occurrences (fingerprint, timestamp, release_tag, trace_id, tags) VALUES (?,?,?,?,?)`,
		fp, now, ev.Release, ev.TraceID(), tagsJSON)
	if err != nil {
		return "", err
	}

	return fp, tx.Commit()
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

	// Need either an exception or a message to store
	hasException := event.Exception != nil && len(event.Exception.Values) > 0
	hasMessage := event.messageText() != ""
	if !hasException && !hasMessage {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok"}`))
		return
	}

	fp, err := storeEvent(event)
	if err != nil {
		log.Printf("store event: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	resp, _ := json.Marshal(map[string]string{"id": fp})
	_, _ = w.Write(resp)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := db.Ping(); err != nil {
		http.Error(w, "db unhealthy", http.StatusServiceUnavailable)
		return
	}
	_, _ = w.Write([]byte("ok"))
}
