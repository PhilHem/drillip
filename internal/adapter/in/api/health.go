package api

import (
	"log/slog"
	"net/http"

	"github.com/PhilHem/drillip/internal/adapter/httpwire"
)

func (h *Handler) HandleHealthDetails(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.Database == nil {
		writeError(w, http.StatusServiceUnavailable, "database health details are unavailable")
		return
	}
	history, err := h.Database.DatabaseHistory(r.Context())
	if err != nil {
		slog.Error("database health details failed", "err", err)
		writeError(w, http.StatusServiceUnavailable, "database health details failed; check the server logs")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, httpwire.HealthDetails{Status: "ok", LastBackupGeneratedAt: history.LastBackupGeneratedAt,
		LastRestoredAt: history.LastRestoredAt, RestoredSnapshotAt: history.RestoredSnapshotAt})
}
