package api

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PhilHem/drillip/internal/domain"
)

type failedDatabaseHistory struct{}

func (failedDatabaseHistory) DatabaseHistory(context.Context) (domain.DatabaseHistory, error) {
	return domain.DatabaseHistory{}, errors.New("secret database path")
}

func TestHealthDetailsUnavailableAndFailure(t *testing.T) {
	for _, h := range []*Handler{{}, {Database: failedDatabaseHistory{}}} {
		w := httptest.NewRecorder()
		h.HandleHealthDetails(w, httptest.NewRequest("GET", "/api/0/health/", nil))
		if w.Code != 503 || strings.Contains(w.Body.String(), "secret") {
			t.Fatalf("status=%d body=%s", w.Code, w.Body)
		}
	}
	w := httptest.NewRecorder()
	(&Handler{}).HandleHealthDetails(w, httptest.NewRequest("POST", "/api/0/health/", nil))
	if w.Code != 405 || w.Header().Get("Allow") != "GET" {
		t.Fatalf("status=%d header=%v", w.Code, w.Header())
	}
}
