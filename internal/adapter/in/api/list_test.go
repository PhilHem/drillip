package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/PhilHem/drillip/internal/adapter/httpwire"
	inport "github.com/PhilHem/drillip/internal/application/port/in"
	"github.com/PhilHem/drillip/internal/domain"
)

type listRecorder struct {
	inport.Errors
	query domain.ListQuery
}

func (r *listRecorder) List(_ context.Context, query domain.ListQuery) (domain.ErrorPage, error) {
	r.query = query
	return domain.ErrorPage{}, nil
}

func TestListDefaultsAndEmptyResponse(t *testing.T) {
	recorder := &listRecorder{}
	h := &Handler{Errors: recorder}
	w := httptest.NewRecorder()
	h.Routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/0/list/", nil))
	if w.Code != http.StatusOK || recorder.query.Limit != domain.DefaultListLimit || recorder.query.Offset != 0 {
		t.Fatalf("status=%d query=%+v body=%s", w.Code, recorder.query, w.Body)
	}
	var response map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if string(response["errors"]) != "[]" || string(response["has_more"]) != "false" {
		t.Fatalf("empty page: %s", w.Body)
	}
}

func TestListRejectsInvalidInputsBeforeApplication(t *testing.T) {
	h := &Handler{} // Any application access would panic.
	for _, query := range []string{
		"sort=unknown", "sort=count%20DESC", "limit=0", "limit=-1", "limit=501",
		"limit=", "limit=abc", "limit=999999999999999999999999999",
		"offset=-1", "offset=", "offset=abc", "offset=999999999999999999999999999",
		"tag=", "tag=service", "tag=%3Dcheckout",
	} {
		t.Run(query, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.Routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/0/list/?"+query, nil))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
		})
	}
	w := httptest.NewRecorder()
	h.Routes().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/0/list/", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status=%d", w.Code)
	}
}

func TestListCapabilityPreservesBaseVersion(t *testing.T) {
	w := httptest.NewRecorder()
	(&Handler{}).Routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/0/capabilities/", nil))
	var capabilities httpwire.Capabilities
	if err := json.Unmarshal(w.Body.Bytes(), &capabilities); err != nil {
		t.Fatal(err)
	}
	if capabilities.CommandAPI != 1 || !slices.Contains(capabilities.Features, httpwire.FeatureErrorList) {
		t.Fatalf("capabilities=%+v", capabilities)
	}
	// Existing clients only decode command_api and ignore additive fields.
	var legacy struct {
		CommandAPI int `json:"command_api"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &legacy); err != nil || legacy.CommandAPI != 1 {
		t.Fatalf("legacy=%+v err=%v", legacy, err)
	}
}
