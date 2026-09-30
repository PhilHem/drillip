package httpclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/PhilHem/drillip/internal/domain"
)

func TestListSearchAndPagesRoundTripThroughServer(t *testing.T) {
	client, _, store := liveClient(t)
	search := "GATEWAY 100%_ & + 'ü"
	filter := domain.ListFilter{Level: "error", TagKey: "tenant", TagVal: "a&b+c/ü"}
	var fingerprints []string
	for i := 0; i < 3; i++ {
		event := &domain.Event{Message: fmt.Sprintf("Payment gateway 100%%_ & + 'ü failure %d", i), Level: "error", Tags: map[string]string{"tenant": filter.TagVal}}
		var fp string
		for occurrence := 0; occurrence < 3-i; occurrence++ {
			result, err := store.StoreEvent(event)
			if err != nil {
				t.Fatal(err)
			}
			fp = result.Fingerprint
		}
		fingerprints = append(fingerprints, fp)
	}
	for _, event := range []*domain.Event{
		{Message: "Payment gateway 100%_ & + 'ü other level", Level: "warning", Tags: map[string]string{"tenant": filter.TagVal}},
		{Message: "Payment gateway 100%_ & + 'ü other tenant", Level: "error", Tags: map[string]string{"tenant": "other"}},
		{Message: "Payment gateway 100xx & + 'ü wildcard near miss", Level: "error", Tags: map[string]string{"tenant": filter.TagVal}},
	} {
		if _, err := store.StoreEvent(event); err != nil {
			t.Fatal(err)
		}
	}
	query := domain.ListQuery{Filter: filter, Search: search, Sort: domain.SortCount, Limit: 1}
	for offset := 0; offset < 4; offset++ {
		query.Offset = offset
		page, err := client.List(context.Background(), query)
		if err != nil {
			t.Fatal(err)
		}
		if offset == 3 {
			if len(page.Errors) != 0 || page.HasMore {
				t.Fatalf("past end: %+v", page)
			}
			continue
		}
		if len(page.Errors) != 1 || page.Errors[0].Fingerprint != fingerprints[offset] || page.HasMore != (offset < 2) {
			t.Fatalf("offset=%d page=%+v", offset, page)
		}
	}
}

func TestListUnsupportedByOldServerDoesNotFallback(t *testing.T) {
	var listCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/0/capabilities/":
			fmt.Fprint(w, `{"command_api":1}`)
		case "/api/0/top/":
			fmt.Fprint(w, `[]`)
		default:
			listCalls.Add(1)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	client, _ := New(srv.URL)
	_, err := client.List(context.Background(), domain.ListQuery{Limit: 50})
	if err == nil || !strings.Contains(err.Error(), "upgrade") || listCalls.Load() != 0 {
		t.Fatalf("err=%v list calls=%d", err, listCalls.Load())
	}
	if _, err := client.ListTop(context.Background(), domain.ListFilter{}, 10); err != nil {
		t.Fatalf("existing top command rejected old server: %v", err)
	}
}

func TestListValidatesBeforeNetwork(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer srv.Close()
	client, _ := New(srv.URL)
	for _, query := range []domain.ListQuery{{Limit: 0}, {Limit: 501}, {Limit: 1, Offset: -1}, {Limit: 1, Sort: "unknown"}} {
		_, err := client.List(context.Background(), query)
		if !errors.Is(err, domain.ErrInvalidListQuery) {
			t.Fatalf("query=%+v err=%v", query, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("network calls=%d", calls.Load())
	}
}

func TestListRejectsMalformedPages(t *testing.T) {
	for _, body := range []string{
		`{}`, `null`, `{"errors":null,"has_more":false}`, `{"errors":[],"has_more":null}`,
		`{"errors":[],"has_more":"false"}`, `{"errors":[],"has_more":true}`,
		`{"errors":[{"fingerprint":"abcd"}],"has_more":false}`,
		`{"errors":[{"fingerprint":"abcd123456789012"},{"fingerprint":"abcd123456789013"}],"has_more":false}`,
	} {
		t.Run(body, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/0/capabilities/" {
					fmt.Fprint(w, `{"command_api":1,"features":["error_list"]}`)
					return
				}
				fmt.Fprint(w, body)
			}))
			defer srv.Close()
			client, _ := New(srv.URL)
			if _, err := client.List(context.Background(), domain.ListQuery{Limit: 1}); err == nil {
				t.Fatalf("accepted %s", body)
			}
		})
	}
}
