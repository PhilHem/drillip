package ingest

import (
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/PhilHem/drillip/domain"
	"github.com/PhilHem/drillip/notify"
	"github.com/PhilHem/drillip/store"
	"github.com/andybalholm/brotli"
)

// EnvelopeItemHeader is the Sentry envelope item header.
type EnvelopeItemHeader struct {
	Type   string `json:"type"`
	Length int    `json:"length"`
}

func parseEnvelope(body []byte) (*domain.Event, error) {
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
			var ev domain.Event
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

// MakeHandler returns an http.HandlerFunc that ingests Sentry events.
// If notifier is nil, notifications are skipped.
func MakeHandler(s *store.Store, notifier *notify.Notifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
			var ev domain.Event
			if jsonErr := json.Unmarshal(body, &ev); jsonErr != nil {
				http.Error(w, "invalid payload", http.StatusBadRequest)
				return
			}
			event = &ev
		}

		// Need either an exception or a message to store
		hasException := event.Exception != nil && len(event.Exception.Values) > 0
		hasMessage := event.MessageText() != ""
		if !hasException && !hasMessage {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"ok"}`))
			return
		}

		result, err := s.StoreEvent(event)
		if err != nil {
			log.Printf("store event: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		if (result.IsNew || result.IsRegression) && notifier != nil {
			go notifier.NotifyNewError(event, result.Fingerprint, result.IsRegression, result.ResolvedDuration)
		}

		w.Header().Set("Content-Type", "application/json")
		resp, _ := json.Marshal(map[string]string{"id": result.Fingerprint})
		_, _ = w.Write(resp)
	}
}

// HandleHealth returns an http.HandlerFunc that checks database health.
func HandleHealth(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(); err != nil {
			http.Error(w, "db unhealthy", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}
}
