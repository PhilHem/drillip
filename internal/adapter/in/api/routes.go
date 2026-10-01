package api

import (
	"net/http"

	"github.com/PhilHem/drillip/internal/adapter/httpwire"
)

// Routes exposes the JSON command API, including its compatibility contract.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/0/capabilities/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		features := []string{httpwire.FeatureErrorList}
		if h.Database != nil {
			features = append(features, httpwire.FeatureDatabaseHistory)
		}
		if h.Backups != nil {
			features = append(features, httpwire.FeatureDatabaseBackup)
		}
		writeJSON(w, httpwire.Capabilities{CommandAPI: 1, Features: features})
	})
	mux.HandleFunc("/api/0/list/", h.HandleList)
	mux.HandleFunc("/api/0/backup/", h.HandleBackup)
	mux.HandleFunc("/api/0/health/", h.HandleHealthDetails)
	mux.HandleFunc("/api/0/top/", h.HandleTop)
	mux.HandleFunc("/api/0/recent/", h.HandleRecent)
	mux.HandleFunc("/api/0/show/", h.HandleShow)
	mux.HandleFunc("/api/0/trend/", h.HandleTrend)
	mux.HandleFunc("/api/0/releases/", h.HandleReleases)
	mux.HandleFunc("/api/0/stats/", h.HandleStats)
	mux.HandleFunc("/api/0/gc/", h.HandleGC)
	mux.HandleFunc("/api/0/resolve/", h.HandleResolve)
	mux.HandleFunc("/api/0/correlate/", h.HandleCorrelate)
	mux.HandleFunc("/api/0/test-email/", h.HandleTestEmail)
	mux.HandleFunc("/api/0/silence/", h.HandleSilence)
	mux.HandleFunc("/api/0/silences/", h.HandleListSilences)
	return mux
}
