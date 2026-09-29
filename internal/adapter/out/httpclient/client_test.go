package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PhilHem/drillip/internal/adapter/in/api"
	store "github.com/PhilHem/drillip/internal/adapter/out/sqlite"
	inport "github.com/PhilHem/drillip/internal/application/port/in"
	"github.com/PhilHem/drillip/internal/application/service"
	"github.com/PhilHem/drillip/internal/domain"
)

func liveClient(t *testing.T) (*Client, *service.Errors, *store.Store) {
	t.Helper()
	s, err := store.Open(t.TempDir() + "/server.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	app := service.New(s, nil, nil)
	h := &api.Handler{Errors: app, Correlation: app}
	srv := httptest.NewServer(h.Routes())
	t.Cleanup(srv.Close)
	client, err := New(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return client, app, s
}
func TestClientRoundTripsRealCommandAPI(t *testing.T) {
	client, app, s := liveClient(t)
	ctx := context.Background()
	var fp string
	for i := 0; i < 3; i++ {
		event := &domain.Event{Message: fmt.Sprintf("failure %d", i), Level: "error", Release: "v1", Tags: map[string]string{"tenant": "a&b+c/ü"}}
		if i == 0 {
			event.Exception = &domain.ExceptionData{Values: []domain.ExceptionValue{{Type: "CheckoutError", Value: "checkout", Stacktrace: &domain.Stacktrace{Frames: []domain.Frame{{Filename: "app.py", Function: "checkout", Lineno: 4}}}}}}
		}
		result, err := s.StoreEvent(event)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			fp = result.Fingerprint
		}
	}
	filter := domain.ListFilter{TagKey: "tenant", TagVal: "a&b+c/ü"}
	top, err := client.ListTop(ctx, filter, 2)
	if err != nil || len(top) != 2 {
		t.Fatalf("top=%+v err=%v", top, err)
	}
	recent, err := client.ListRecent(ctx, filter, time.Now().Add(-time.Hour))
	if err != nil || len(recent) != 3 || recent[0].FirstSeen == "" {
		t.Fatalf("recent=%+v err=%v", recent, err)
	}
	actual, err := client.GetDetail(ctx, fp[:8])
	expected, _ := app.GetDetail(ctx, fp)
	if err != nil || !reflect.DeepEqual(actual, expected) {
		t.Fatalf("detail=%+v expected=%+v err=%v", actual, expected, err)
	}
	trend, err := client.GetTrend(ctx, fp[:8], time.Now().Add(-time.Hour))
	if err != nil || trend.Fingerprint != fp || len(trend.Buckets) != 1 {
		t.Fatalf("trend=%+v err=%v", trend, err)
	}
	releases, err := client.GetReleases(ctx, fp[:8])
	if err != nil || releases.Fingerprint != fp || len(releases.Releases) != 1 {
		t.Fatalf("releases=%+v err=%v", releases, err)
	}
	correlation, err := client.Correlate(ctx, inport.CorrelateQuery{Fingerprint: fp[:8], Nth: 1})
	if err != nil || correlation.Error.Fingerprint != fp || correlation.Occurrence == nil || correlation.Error.Stacktrace == "" {
		t.Fatalf("correlation=%+v err=%v", correlation, err)
	}
	expiry := time.Now().Add(time.Hour)
	reason := "planned & a+b / ü"
	silenced, err := client.Silence(ctx, fp[:8], &expiry, reason)
	if err != nil || silenced.ExpiresAt == nil || !silenced.ExpiresAt.Equal(expiry.UTC().Truncate(time.Second)) {
		t.Fatalf("silenced=%+v err=%v", silenced, err)
	}
	silences, err := client.ListSilences(ctx)
	if err != nil || len(silences) != 1 || silences[0].Reason != reason || silences[0].ExpiresAt != silenced.ExpiresAt.Format(time.RFC3339) {
		t.Fatalf("silences=%+v err=%v", silences, err)
	}
	if full, err := client.Unsilence(ctx, fp[:8]); err != nil || full != fp {
		t.Fatalf("unsilence=%s err=%v", full, err)
	}
	if err := s.Silence("dead12", nil, "legacy"); err != nil {
		t.Fatal(err)
	}
	if full, err := client.Unsilence(ctx, "dead12"); err != nil || full != "dead12" {
		t.Fatalf("legacy=%s err=%v", full, err)
	}
	if result, err := client.Resolve(ctx, fp[:8]); err != nil || result.Fingerprint != fp {
		t.Fatalf("resolve=%+v err=%v", result, err)
	}
	if st, err := client.GetStats(ctx); err != nil || st.UniqueErrors != 3 {
		t.Fatalf("stats=%+v err=%v", st, err)
	}
	// A past instant remains past even when its printed local clock is ahead of UTC.
	pastOffset := time.Now().Add(-time.Hour).In(time.FixedZone("plus12", 12*60*60))
	if deleted, err := client.GCOccurrences(ctx, pastOffset); err != nil || deleted != 0 {
		t.Fatalf("offset gc=%d err=%v", deleted, err)
	}
	if deleted, err := client.GCOccurrences(ctx, time.Now().Add(time.Hour)); err != nil || deleted != 3 {
		t.Fatalf("gc=%d err=%v", deleted, err)
	}
	trend, err = client.GetTrend(ctx, fp[:8], time.Now().Add(-time.Hour))
	if err != nil || trend.Fingerprint != fp || len(trend.Buckets) != 0 {
		t.Fatalf("empty trend=%+v err=%v", trend, err)
	}
	releases, err = client.GetReleases(ctx, fp[:8])
	if err != nil || releases.Fingerprint != fp || len(releases.Releases) != 0 {
		t.Fatalf("empty releases=%+v err=%v", releases, err)
	}
}

type clockRecorder struct {
	inport.Errors
	since, before, expiry time.Time
	limit                 int
}

func (r *clockRecorder) ListTop(_ context.Context, _ domain.ListFilter, n int) ([]domain.ErrorSummary, error) {
	r.limit = n
	return nil, nil
}
func (r *clockRecorder) ListRecent(_ context.Context, _ domain.ListFilter, t time.Time) ([]domain.ErrorSummary, error) {
	r.since = t
	return nil, nil
}
func (r *clockRecorder) GetTrend(_ context.Context, _ string, t time.Time) (domain.Trend, error) {
	r.since = t
	return domain.Trend{Fingerprint: "abcd123456789012"}, nil
}
func (r *clockRecorder) GCOccurrences(_ context.Context, t time.Time) (int64, error) {
	r.before = t
	return 0, nil
}
func (r *clockRecorder) Silence(_ context.Context, _ string, t *time.Time, _ string) (domain.SilenceResult, error) {
	r.expiry = *t
	return domain.SilenceResult{Fingerprint: "abcd123456789012", ExpiresAt: t}, nil
}
func TestAbsoluteTimesReachApplicationWithoutRounding(t *testing.T) {
	recorder := &clockRecorder{}
	h := &api.Handler{Errors: recorder}
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()
	c, _ := New(srv.URL)
	ctx := context.Background()
	at := time.Date(2026, 9, 29, 10, 11, 12, 987654321, time.FixedZone("offset", 90*60))
	if _, err := c.ListTop(ctx, domain.ListFilter{}, 47); err != nil || recorder.limit != 47 {
		t.Fatalf("limit=%d err=%v", recorder.limit, err)
	}
	if _, err := c.ListRecent(ctx, domain.ListFilter{}, at); err != nil || !recorder.since.Equal(at) {
		t.Fatalf("recent=%v err=%v", recorder.since, err)
	}
	if _, err := c.GetTrend(ctx, "abcd", at); err != nil || !recorder.since.Equal(at) {
		t.Fatalf("trend=%v err=%v", recorder.since, err)
	}
	if _, err := c.GCOccurrences(ctx, at); err != nil || !recorder.before.Equal(at) {
		t.Fatalf("gc=%v err=%v", recorder.before, err)
	}
	if _, err := c.Silence(ctx, "abcd", &at, ""); err != nil || !recorder.expiry.Equal(at) {
		t.Fatalf("silence=%v err=%v", recorder.expiry, err)
	}
}
func TestCompatibilityCheckPreventsOldServerMutation(t *testing.T) {
	for _, body := range []string{"ok", `{}`, `{"command_api":null}`, `{"command_api":2}`, `{"command_api":"1"}`} {
		t.Run(body, func(t *testing.T) {
			var mutations atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					mutations.Add(1)
				}
				fmt.Fprint(w, body)
			}))
			defer srv.Close()
			c, _ := New(srv.URL)
			expires := time.Now().Add(time.Hour)
			_, err := c.Silence(context.Background(), "abcd", &expires, "")
			if err == nil || !strings.Contains(err.Error(), "upgrade") || mutations.Load() != 0 {
				t.Fatalf("err=%v mutations=%d", err, mutations.Load())
			}
		})
	}
}
func TestClientRejectsMalformedResponsesAndRedirects(t *testing.T) {
	operations := map[string]func(*Client) error{
		"show":      func(c *Client) error { _, e := c.GetDetail(context.Background(), "abcd"); return e },
		"trend":     func(c *Client) error { _, e := c.GetTrend(context.Background(), "abcd", time.Now()); return e },
		"releases":  func(c *Client) error { _, e := c.GetReleases(context.Background(), "abcd"); return e },
		"stats":     func(c *Client) error { _, e := c.GetStats(context.Background()); return e },
		"resolve":   func(c *Client) error { _, e := c.Resolve(context.Background(), "abcd"); return e },
		"silence":   func(c *Client) error { _, e := c.Silence(context.Background(), "abcd", nil, ""); return e },
		"unsilence": func(c *Client) error { _, e := c.Unsilence(context.Background(), "abcd"); return e },
		"gc":        func(c *Client) error { _, e := c.GCOccurrences(context.Background(), time.Now()); return e },
		"correlate": func(c *Client) error {
			_, e := c.Correlate(context.Background(), inport.CorrelateQuery{Fingerprint: "abcd", Nth: 1})
			return e
		},
	}
	for name, operation := range operations {
		t.Run(name, func(t *testing.T) {
			for _, body := range []string{`{}`, `null`, `{"fingerprint":"a"}`, `{"fingerprint":"abcd123456789012"} trailing`} {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.Contains(r.URL.Path, "capabilities") {
						fmt.Fprint(w, `{"command_api":1}`)
						return
					}
					fmt.Fprint(w, body)
				}))
				c, _ := New(srv.URL)
				err := operation(c)
				srv.Close()
				if err == nil {
					t.Fatalf("accepted %s", body)
				}
			}
		})
	}
	for _, code := range []int{301, 302, 303, 307, 308} {
		var followed atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "capabilities") {
				fmt.Fprint(w, `{"command_api":1}`)
				return
			}
			if r.URL.Path == "/redirected" {
				followed.Add(1)
				return
			}
			w.Header().Set("Location", "/redirected")
			w.WriteHeader(code)
		}))
		c, _ := New(srv.URL)
		_, err := c.Resolve(context.Background(), "abcd")
		srv.Close()
		if err == nil || followed.Load() != 0 {
			t.Fatalf("status=%d err=%v followed=%d", code, err, followed.Load())
		}
	}
}
func TestDeadlineCoversCompatibilityAndOperation(t *testing.T) {
	for _, stallCapability := range []bool{true, false} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "capabilities") && !stallCapability {
				fmt.Fprint(w, `{"command_api":1}`)
				return
			}
			<-r.Context().Done()
		}))
		c, _ := New(srv.URL)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		_, err := c.Resolve(ctx, "abcd")
		cancel()
		srv.Close()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err=%v", err)
		}
	}
}
func TestHTTPErrorContracts(t *testing.T) {
	for _, status := range []int{400, 404, 409, 500} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "capabilities") {
				fmt.Fprint(w, `{"command_api":1}`)
				return
			}
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(map[string]string{"error": "failed operation"})
		}))
		c, _ := New(srv.URL)
		_, err := c.GetDetail(context.Background(), "abcd")
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), fmt.Sprint(status)) {
			t.Fatalf("status=%d err=%v", status, err)
		}
		if status == 400 && errors.Is(err, domain.ErrInvalidFingerprint) {
			t.Fatal("misclassified non-reference error")
		}
		if status == 404 && !errors.Is(err, domain.ErrErrorNotFound) {
			t.Fatal(err)
		}
		if status == 409 && !errors.Is(err, domain.ErrAmbiguousFingerprint) {
			t.Fatal(err)
		}
	}
}
func TestHTTPSAndPathPrefix(t *testing.T) {
	var paths []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if strings.Contains(r.URL.Path, "capabilities") {
			fmt.Fprint(w, `{"command_api":1}`)
			return
		}
		fmt.Fprint(w, `{"unique_errors":0,"total_occurrences":0}`)
	}))
	defer srv.Close()
	c, _ := New(srv.URL + "/drillip")
	c.http.Transport = srv.Client().Transport
	if _, err := c.GetStats(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(paths, []string{"/drillip/api/0/capabilities/", "/drillip/api/0/stats/"}) {
		t.Fatal(paths)
	}
	for _, invalid := range []string{"localhost:8300", "ftp://example.com", "http://user:pass@example.com", "http://example.com/?x=1"} {
		if _, err := New(invalid); err == nil {
			t.Fatal(invalid)
		}
	}
}

type fullCorrelation struct{}

func (fullCorrelation) Correlate(_ context.Context, q inport.CorrelateQuery) (*domain.Correlation, error) {
	return &domain.Correlation{Error: domain.CorrelateData{Fingerprint: "abcd123456789012", Type: "Checkout", Value: "failed", Stacktrace: `{"frames":[]}`, Breadcrumbs: `[{"message":"started"}]`, UserContext: `{"id":"42"}`}, Occurrence: &domain.CorrelatedOccurrence{Nth: q.Nth, Timestamp: "2026-09-29T10:00:00Z", TraceID: "trace-id"}, Logs: []domain.JournalEntry{{Timestamp: "123", Message: "server log", Priority: "3"}}, Trace: &domain.TraceData{ServiceName: "checkout", Spans: []domain.TraceSpan{{OperationName: "charge", Duration: 1500 * time.Microsecond}}}, Metrics: &domain.MetricsSnapshot{Values: map[string]string{"requests": "42"}}, Profile: []domain.ProfileEntry{{Function: "charge"}}}, nil
}
func TestCorrelationOptionalDataSurvivesTransport(t *testing.T) {
	h := &api.Handler{Correlation: fullCorrelation{}}
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()
	c, _ := New(srv.URL)
	result, err := c.Correlate(context.Background(), inport.CorrelateQuery{Fingerprint: "abcd", Nth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.Occurrence.Nth != 2 || result.Occurrence.TraceID != "trace-id" || result.Occurrence.Time.IsZero() || result.Logs[0].Message != "server log" || result.Trace.Spans[0].Duration != 1500*time.Microsecond || result.Metrics.Values["requests"] != "42" || result.Profile[0].Function != "charge" || result.Error.UserContext != `{"id":"42"}` {
		t.Fatalf("lost correlation data: %+v", result)
	}
}
func TestReferencesRemainConsistentThroughClient(t *testing.T) {
	c, _, s := liveClient(t)
	ctx := context.Background()
	seen := map[byte]string{}
	var prefix, first, second string
	for i := 0; i < 17; i++ {
		result, err := s.StoreEvent(&domain.Event{Message: fmt.Sprint("collision", i)})
		if err != nil {
			t.Fatal(err)
		}
		if fp, ok := seen[result.Fingerprint[0]]; ok {
			prefix = result.Fingerprint[:1]
			first, second = fp, result.Fingerprint
			break
		}
		seen[result.Fingerprint[0]] = result.Fingerprint
	}
	if prefix == "" {
		t.Fatal("missing collision")
	}
	for name, operation := range map[string]func(string) error{
		"show": func(ref string) error { _, e := c.GetDetail(ctx, ref); return e }, "trend": func(ref string) error { _, e := c.GetTrend(ctx, ref, time.Now()); return e }, "releases": func(ref string) error { _, e := c.GetReleases(ctx, ref); return e }, "correlate": func(ref string) error {
			_, e := c.Correlate(ctx, inport.CorrelateQuery{Fingerprint: ref, Nth: 1})
			return e
		}, "resolve": func(ref string) error { _, e := c.Resolve(ctx, ref); return e }, "silence": func(ref string) error { _, e := c.Silence(ctx, ref, nil, ""); return e }, "unsilence": func(ref string) error { _, e := c.Unsilence(ctx, ref); return e },
	} {
		t.Run(name, func(t *testing.T) {
			if err := operation(prefix); !errors.Is(err, domain.ErrAmbiguousFingerprint) {
				t.Fatalf("ambiguous: %v", err)
			}
			if err := operation("bad!"); !errors.Is(err, domain.ErrInvalidFingerprint) {
				t.Fatalf("invalid: %v", err)
			}
			if err := operation("0000000000000000"); !errors.Is(err, domain.ErrErrorNotFound) {
				t.Fatalf("unknown: %v", err)
			}
		})
	}
	for _, fp := range []string{first, second} {
		d, _ := s.GetDetail(fp)
		if d.ResolvedAt != "" || s.IsSilenced(fp) {
			t.Fatalf("ambiguous mutation changed %s", fp)
		}
	}
}
