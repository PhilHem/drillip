package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/PhilHem/drillip/internal/domain"
)

func (h *Handler) HandleBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.Backups == nil {
		writeError(w, http.StatusServiceUnavailable, "database backups are unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	// A slow receiver must not keep the temporary snapshot indefinitely.
	controller := http.NewResponseController(w)
	if err := controller.SetWriteDeadline(time.Now().Add(2 * time.Minute)); err == nil {
		defer controller.SetWriteDeadline(time.Time{})
	}
	backup, err := h.Backups.Backup(ctx)
	if err != nil {
		if errors.Is(err, domain.ErrBackupBusy) {
			w.Header().Set("Retry-After", "5")
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		slog.Error("database backup failed", "err", err)
		writeError(w, http.StatusInternalServerError, "database backup failed; check the server logs")
		return
	}
	defer func() {
		if err := backup.Close(); err != nil {
			slog.Error("database backup cleanup failed", "err", err)
		}
	}()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="drillip-backup.db"`)
	w.Header().Set("Content-Length", strconv.FormatInt(backup.Size(), 10))
	w.Header().Set("Cache-Control", "no-store")
	if _, err := io.Copy(w, backup); err != nil {
		slog.Error("database backup download failed", "err", err)
	}
}
