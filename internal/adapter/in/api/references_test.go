package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReferenceContractAcrossHTTP(t *testing.T) {
	s := setupStore(t)
	seedReference(t, s, "abcd111111111111")
	seedReference(t, s, "abcd222222222222")
	h := testHandler(s)
	routes := []struct {
		method, path string
		handle       http.HandlerFunc
	}{
		{"GET", "show", h.HandleShow}, {"GET", "trend", h.HandleTrend},
		{"GET", "releases", h.HandleReleases}, {"GET", "correlate", h.HandleCorrelate},
		{"POST", "resolve", h.HandleResolve}, {"POST", "silence", h.HandleSilence},
		{"DELETE", "silence", h.HandleSilence},
	}
	for _, route := range routes {
		for ref, status := range map[string]int{"abcd": 409, "eeee": 404} {
			w := httptest.NewRecorder()
			route.handle(w, httptest.NewRequest(route.method, "/api/0/"+route.path+"/"+ref+"/", nil))
			if w.Code != status {
				t.Fatalf("%s %s %s: %d %s", route.method, route.path, ref, w.Code, w.Body.String())
			}
		}
	}
	for _, fp := range []string{"abcd111111111111", "abcd222222222222"} {
		detail, err := s.GetDetail(fp)
		if err != nil || detail.ResolvedAt != "" || s.IsSilenced(fp) {
			t.Fatalf("ambiguous reference changed %s: %v", fp, err)
		}
	}
	for _, route := range routes {
		w := httptest.NewRecorder()
		route.handle(w, httptest.NewRequest(route.method, "/api/0/"+route.path+"/abcd1/", nil))
		if w.Code != 200 {
			t.Fatalf("unique %s %s: %d %s", route.method, route.path, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "abcd111111111111") {
			t.Fatalf("canonical reference absent: %s", w.Body.String())
		}
	}
	untouched, _ := s.GetDetail("abcd222222222222")
	if untouched.ResolvedAt != "" {
		t.Fatal("resolve affected another matching error")
	}
}
