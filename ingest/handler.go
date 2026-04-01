package ingest

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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

const maxDecompressedSize = 10 << 20 // 10 MB

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
	return io.ReadAll(io.LimitReader(reader, maxDecompressedSize))
}

// writeError writes a structured JSON error response.
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// MakeHandler returns an http.HandlerFunc that ingests Sentry events.
// If notifier is nil, notifications are skipped.
func MakeHandler(s *store.Store, notifier *notify.Notifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10 MB

		body, err := readBody(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad request")
			return
		}
		defer r.Body.Close()

		// Try envelope format first, fall back to plain JSON
		event, err := parseEnvelope(body)
		if err != nil {
			var ev domain.Event
			if jsonErr := json.Unmarshal(body, &ev); jsonErr != nil {
				writeError(w, http.StatusBadRequest, "invalid payload")
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
			slog.Error("store event", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		evType := "message"
		if event.Exception != nil && len(event.Exception.Values) > 0 {
			evType = event.Exception.Values[0].Type
		}
		slog.Debug("event stored", "fingerprint", result.Fingerprint, "type", evType, "new", result.IsNew, "regression", result.IsRegression)

		if (result.IsNew || result.IsRegression) && notifier != nil {
			if s.IsSilenced(result.Fingerprint) {
				slog.Info("notify: silenced fingerprint, skipping", "fingerprint", result.Fingerprint[:8])
			} else {
				go notifier.NotifyNewError(event, result.Fingerprint, result.IsRegression, result.ResolvedDuration)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		resp, _ := json.Marshal(map[string]string{"id": result.Fingerprint})
		_, _ = w.Write(resp)
	}
}

// HandleHealth returns an http.HandlerFunc that checks database health.
func HandleHealth(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := s.Ping(); err != nil {
			writeError(w, http.StatusServiceUnavailable, "db unhealthy")
			return
		}
		_, _ = w.Write([]byte("ok"))
	}
}
