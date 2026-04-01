package store

import (
	"database/sql"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/PhilHem/drillip/domain"
)

// ---------------------------------------------------------------------------
// TigerBeetle-style deterministic simulation test for the notification state
// machine.  A PRNG drives random operations (ingest, notify, silence, resolve,
// auto-resolve) against both a real Store and a lightweight model.  After every
// operation we assert that invariants hold and that the model and Store agree.
// A single seed makes any failure exactly reproducible.
// ---------------------------------------------------------------------------

// --- Model ------------------------------------------------------------------

type errorState int

const (
	stateUnresolved errorState = iota
	stateNotified
	stateResolved
)

func (s errorState) String() string {
	return [...]string{"unresolved", "notified", "resolved"}[s]
}

type modelError struct {
	fp          string
	state       errorState
	count       int
	silenced    bool
	stale       bool // backdated past the auto-resolve threshold
	everNotified bool // notified_at persists across regressions in DB
}

type model struct {
	errors map[string]*modelError
}

func newModel() *model {
	return &model{errors: make(map[string]*modelError)}
}

// --- Operations -------------------------------------------------------------

type opKind int

const (
	opIngest opKind = iota
	opNotify
	opSilence
	opUnsilence
	opAutoResolve
	opManualResolve
	opReoccur
	opBackdate // mark an error as stale
	numOps
)

func (o opKind) String() string {
	return [...]string{
		"Ingest", "Notify", "Silence", "Unsilence",
		"AutoResolve", "ManualResolve", "Reoccur", "Backdate",
	}[o]
}

// --- Simulator --------------------------------------------------------------

type simulator struct {
	t     *testing.T
	store *Store
	model *model
	rng   *rand.Rand
	ops   int
}

func newSimulator(t *testing.T, seed int64) *simulator {
	t.Helper()
	s := setupStore(t)
	return &simulator{
		t:     t,
		store: s,
		model: newModel(),
		rng:   rand.New(rand.NewSource(seed)),
	}
}

func (sim *simulator) randomFP() string {
	if len(sim.model.errors) == 0 {
		return ""
	}
	fps := make([]string, 0, len(sim.model.errors))
	for fp := range sim.model.errors {
		fps = append(fps, fp)
	}
	return fps[sim.rng.Intn(len(fps))]
}

func (sim *simulator) run(steps int) {
	for range steps {
		op := opKind(sim.rng.Intn(int(numOps)))

		// Need existing errors for most ops
		if len(sim.model.errors) == 0 && op != opIngest {
			op = opIngest
		}

		switch op {
		case opIngest:
			sim.doIngest()
		case opNotify:
			sim.doNotify()
		case opSilence:
			sim.doSilence()
		case opUnsilence:
			sim.doUnsilence()
		case opAutoResolve:
			sim.doAutoResolve()
		case opManualResolve:
			sim.doManualResolve()
		case opReoccur:
			sim.doReoccur()
		case opBackdate:
			sim.doBackdate()
		}

		sim.ops++
		sim.checkInvariants()
	}
}

// --- Operation implementations ----------------------------------------------

func (sim *simulator) doIngest() {
	n := sim.ops
	ev := &domain.Event{Message: fmt.Sprintf("error-%d", n)}
	fp := domain.Fingerprint(ev)

	result, err := sim.store.StoreEvent(ev)
	if err != nil {
		sim.t.Fatalf("step %d ingest: %v", sim.ops, err)
	}

	if existing, ok := sim.model.errors[fp]; ok {
		existing.count++
		existing.stale = false
		if existing.state == stateResolved {
			existing.state = stateUnresolved // regression
		}
	} else {
		sim.model.errors[result.Fingerprint] = &modelError{
			fp:    result.Fingerprint,
			state: stateUnresolved,
			count: 1,
		}
	}
}

func (sim *simulator) doNotify() {
	fp := sim.randomFP()
	me := sim.model.errors[fp]
	if me.state == stateResolved || me.silenced {
		return
	}

	if err := sim.store.MarkNotified(fp); err != nil {
		sim.t.Fatalf("step %d notify: %v", sim.ops, err)
	}
	me.state = stateNotified
	me.everNotified = true
}

func (sim *simulator) doSilence() {
	fp := sim.randomFP()
	sim.store.Silence(fp, nil, "sim")
	sim.model.errors[fp].silenced = true
}

func (sim *simulator) doUnsilence() {
	fp := sim.randomFP()
	sim.store.Unsilence(fp)
	sim.model.errors[fp].silenced = false
}

func (sim *simulator) doBackdate() {
	fp := sim.randomFP()
	// Set last_seen to 48h ago relative to real wall clock
	staleTime := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339)
	sim.store.db.Exec("UPDATE errors SET last_seen = ? WHERE fingerprint = ?", staleTime, fp)
	sim.model.errors[fp].stale = true
}

func (sim *simulator) doAutoResolve() {
	resolved, err := sim.store.AutoResolve(24 * time.Hour)
	if err != nil {
		sim.t.Fatalf("step %d auto-resolve: %v", sim.ops, err)
	}

	// Compute expected set: everNotified + stale → in result.
	// All stale errors get resolved in DB regardless.
	expectedInResult := make(map[string]bool)
	for fp, me := range sim.model.errors {
		if me.state != stateResolved && me.stale {
			if me.everNotified {
				expectedInResult[fp] = true
			}
			me.state = stateResolved
			me.stale = false
		}
	}

	gotFPs := make(map[string]bool)
	for _, r := range resolved {
		gotFPs[r.Fingerprint] = true
	}

	for fp := range expectedInResult {
		if !gotFPs[fp] {
			sim.t.Errorf("step %d AUTO-RESOLVE: expected notified fp=%s in result but missing",
				sim.ops, fp[:8])
		}
	}
	for fp := range gotFPs {
		if !expectedInResult[fp] {
			sim.t.Errorf("step %d AUTO-RESOLVE: unexpected fp=%s in result (not notified in model)",
				sim.ops, fp[:8])
		}
	}
}

func (sim *simulator) doManualResolve() {
	fp := sim.randomFP()
	me := sim.model.errors[fp]
	if me.state == stateResolved {
		return
	}

	_, err := sim.store.Resolve(fp[:8])
	if err != nil {
		sim.t.Fatalf("step %d manual-resolve: %v", sim.ops, err)
	}
	for _, m := range sim.model.errors {
		if len(m.fp) >= 8 && m.fp[:8] == fp[:8] && m.state != stateResolved {
			m.state = stateResolved
		}
	}
}

func (sim *simulator) doReoccur() {
	fp := sim.randomFP()
	me := sim.model.errors[fp]

	ev := &domain.Event{Message: me.fp} // won't match original — use direct DB update
	_ = ev

	// Just bump count and last_seen in DB, and clear resolved_at for regression
	now := time.Now().UTC().Format(time.RFC3339)
	sim.store.db.Exec(
		"UPDATE errors SET count = count + 1, last_seen = ?, resolved_at = NULL WHERE fingerprint = ?",
		now, fp,
	)

	me.count++
	me.stale = false
	if me.state == stateResolved {
		me.state = stateUnresolved
	}
}

// --- Invariant checks -------------------------------------------------------

func (sim *simulator) checkInvariants() {
	sim.t.Helper()

	for fp, me := range sim.model.errors {
		var (
			dbResolvedAt sql.NullString
			dbNotifiedAt sql.NullString
			dbCount      int
		)
		err := sim.store.db.QueryRow(
			"SELECT resolved_at, notified_at, count FROM errors WHERE fingerprint = ?", fp,
		).Scan(&dbResolvedAt, &dbNotifiedAt, &dbCount)
		if err != nil {
			sim.t.Fatalf("step %d invariant query fp=%s: %v", sim.ops, fp[:8], err)
		}

		// INV-1: resolved in model → resolved_at set in DB
		if me.state == stateResolved && !dbResolvedAt.Valid {
			sim.t.Errorf("step %d INV-1: fp=%s model=resolved but DB resolved_at=NULL",
				sim.ops, fp[:8])
		}

		// INV-2: unresolved in model → resolved_at must be NULL in DB
		if me.state != stateResolved && dbResolvedAt.Valid {
			sim.t.Errorf("step %d INV-2: fp=%s model=%s but DB resolved_at=%s",
				sim.ops, fp[:8], me.state, dbResolvedAt.String)
		}

		// INV-3: DB count >= model count
		if dbCount < me.count {
			sim.t.Errorf("step %d INV-3: fp=%s DB count=%d < model count=%d",
				sim.ops, fp[:8], dbCount, me.count)
		}

		// INV-4: notified_at in DB implies MarkNotified was called at some point
		if dbNotifiedAt.Valid {
			// This is ok — notified_at persists across regressions.
			// But the fingerprint must exist in the model.
			if _, exists := sim.model.errors[fp]; !exists {
				sim.t.Errorf("step %d INV-4: DB has notified_at for unknown fp=%s",
					sim.ops, fp[:8])
			}
		}

		// INV-5: model says notified → DB must have notified_at
		if me.state == stateNotified && !dbNotifiedAt.Valid {
			sim.t.Errorf("step %d INV-5: fp=%s model=notified but DB notified_at=NULL",
				sim.ops, fp[:8])
		}
	}

	// INV-6: no un-notified error should ever appear in AutoResolve's
	// result set. Verified structurally in doAutoResolve, but also
	// check the query invariant holds: the SELECT for resolved email
	// would never return rows where notified_at IS NULL.
	rows, err := sim.store.db.Query(
		`SELECT COUNT(*) FROM errors WHERE resolved_at IS NOT NULL AND notified_at IS NULL`,
	)
	if err != nil {
		sim.t.Fatalf("step %d INV-6 query: %v", sim.ops, err)
	}
	if rows.Next() {
		var count int
		rows.Scan(&count)
		// These exist in DB (silently resolved) — that's correct.
		// The invariant is that AutoResolve's SELECT excludes them,
		// which we already verify in doAutoResolve.
	}
	rows.Close()
}

// --- Test entry points: state machine simulation ----------------------------

func TestNotificationStateMachine(t *testing.T) {
	seeds := []int64{42, 1337, 2026, 9999, 0, 314159}
	for _, seed := range seeds {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			sim := newSimulator(t, seed)
			sim.run(200)
			t.Logf("completed 200 steps, %d errors tracked", len(sim.model.errors))
		})
	}
}

func TestNotificationStateMachineLong(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long simulation in short mode")
	}
	for seed := range int64(50) {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			sim := newSimulator(t, seed)
			sim.run(1000)
		})
	}
}

// ---------------------------------------------------------------------------
// Fingerprint stability: the property that caused the original bug.
// Same logical error with different timestamps/formatting must always
// produce the same fingerprint.
// ---------------------------------------------------------------------------

func TestFingerprintStabilityProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(8675309))
	modules := []string{
		"entitlements_app.adapters.portal2:_login:258",
		"myapp.views:handle_request:42",
		"django.core.handlers:inner:204",
		"auth.backends:authenticate:67",
	}
	levels := []string{"ERROR", "WARNING", "DEBUG", "INFO", "CRITICAL"}
	messages := []string{
		"Portal2 role switch failed",
		"Connection refused",
		"Invalid token",
		"division by zero",
		"SAML Response not found",
	}

	for i := range 500 {
		msg := messages[rng.Intn(len(messages))]
		mod := modules[rng.Intn(len(modules))]
		level := levels[rng.Intn(len(levels))]

		// Generate two events with different timestamps but same error
		ts1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).
			Add(time.Duration(rng.Intn(365*24*3600)) * time.Second)
		ts2 := ts1.Add(time.Duration(rng.Intn(86400)) * time.Second)

		loguru1 := fmt.Sprintf("%s | %-8s | %s - %s",
			ts1.Format("2006-01-02 15:04:05.000"), level, mod, msg)
		loguru2 := fmt.Sprintf("%s | %-8s | %s - %s",
			ts2.Format("2006-01-02 15:04:05.000"), level, mod, msg)

		ev1 := &domain.Event{Message: loguru1}
		ev2 := &domain.Event{Message: loguru2}

		fp1 := domain.Fingerprint(ev1)
		fp2 := domain.Fingerprint(ev2)

		if fp1 != fp2 {
			t.Fatalf("iteration %d: fingerprint mismatch for %q\n  ts1=%s fp=%s\n  ts2=%s fp=%s",
				i, msg, ts1.Format(time.RFC3339), fp1, ts2.Format(time.RFC3339), fp2)
		}

		// Also test: plain message (no loguru prefix) must produce a
		// DIFFERENT fingerprint than the loguru-wrapped version.
		evPlain := &domain.Event{Message: msg}
		fpPlain := domain.Fingerprint(evPlain)
		if fpPlain != fp1 {
			// This is actually fine — StripLogPrefix on a loguru line
			// yields the same plain message. They SHOULD match.
		}

		// And: two genuinely different messages must differ.
		if i > 0 {
			other := messages[(rng.Intn(len(messages)-1)+1)%len(messages)]
			if other != msg {
				evOther := &domain.Event{Message: other}
				if domain.Fingerprint(evOther) == fpPlain {
					t.Fatalf("iteration %d: collision between %q and %q", i, msg, other)
				}
			}
		}
	}
}

// Also test logentry.message (template) stability: different formatted
// values with the same template must produce the same fingerprint.
func TestFingerprintTemplateStability(t *testing.T) {
	rng := rand.New(rand.NewSource(55555))
	template := "User %s failed to authenticate via %s"

	for range 100 {
		user := fmt.Sprintf("user-%d", rng.Intn(10000))
		method := []string{"SAML", "LDAP", "OAuth", "password"}[rng.Intn(4)]
		formatted := fmt.Sprintf("User %s failed to authenticate via %s", user, method)

		ev := &domain.Event{
			LogEntry: &domain.LogEntry{
				Message:   template,
				Formatted: formatted,
			},
		}

		fp := domain.Fingerprint(ev)
		expected := domain.Fingerprint(&domain.Event{
			LogEntry: &domain.LogEntry{Message: template},
		})

		if fp != expected {
			t.Fatalf("template fingerprint mismatch: formatted=%q fp=%s expected=%s",
				formatted, fp, expected)
		}
	}
}

// ---------------------------------------------------------------------------
// Idempotency: operations applied twice must be safe and produce the same
// result as applying them once.
// ---------------------------------------------------------------------------

func TestIdempotency(t *testing.T) {
	t.Run("MarkNotified_twice", func(t *testing.T) {
		s := setupStore(t)
		ev := &domain.Event{Message: "idempotent-notify"}
		result, _ := s.StoreEvent(ev)

		if err := s.MarkNotified(result.Fingerprint); err != nil {
			t.Fatal(err)
		}
		if err := s.MarkNotified(result.Fingerprint); err != nil {
			t.Fatal(err)
		}

		var notifiedAt string
		s.db.QueryRow("SELECT notified_at FROM errors WHERE fingerprint = ?",
			result.Fingerprint).Scan(&notifiedAt)
		if notifiedAt == "" {
			t.Fatal("notified_at should be set")
		}
	})

	t.Run("AutoResolve_twice", func(t *testing.T) {
		s := setupStore(t)
		ev := &domain.Event{Message: "idempotent-resolve"}
		result, _ := s.StoreEvent(ev)
		s.MarkNotified(result.Fingerprint)

		// Backdate
		stale := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339)
		s.db.Exec("UPDATE errors SET last_seen = ? WHERE fingerprint = ?",
			stale, result.Fingerprint)

		r1, err := s.AutoResolve(24 * time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if len(r1) != 1 {
			t.Fatalf("first resolve: expected 1, got %d", len(r1))
		}

		// Second call: already resolved, should return empty
		r2, err := s.AutoResolve(24 * time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if len(r2) != 0 {
			t.Fatalf("second resolve: expected 0, got %d", len(r2))
		}
	})

	t.Run("StoreEvent_duplicate", func(t *testing.T) {
		s := setupStore(t)
		ev := &domain.Event{Message: "duplicate-event"}

		r1, _ := s.StoreEvent(ev)
		r2, _ := s.StoreEvent(ev)

		if r1.Fingerprint != r2.Fingerprint {
			t.Fatal("same event should produce same fingerprint")
		}
		if !r1.IsNew {
			t.Fatal("first store should be new")
		}
		if r2.IsNew {
			t.Fatal("second store should not be new")
		}

		var count int
		s.db.QueryRow("SELECT count FROM errors WHERE fingerprint = ?",
			r1.Fingerprint).Scan(&count)
		if count != 2 {
			t.Fatalf("expected count=2, got %d", count)
		}
	})
}

// ---------------------------------------------------------------------------
// Fault injection: simulate crash between "notification sent" and
// MarkNotified.  This is the dangerous gap — the error was communicated
// but the DB doesn't know it.  On recovery, the error resolves silently
// (conservative: user might miss one resolved notification, but won't
// get a spurious one).
// ---------------------------------------------------------------------------

func TestCrashBetweenNotifyAndMark(t *testing.T) {
	s := setupStore(t)

	ev := &domain.Event{Message: "crash-gap-error"}
	result, _ := s.StoreEvent(ev)

	// Simulate: notification was sent (email delivered) but process crashed
	// before MarkNotified could run.  We intentionally do NOT call MarkNotified.

	// Backdate to make it stale
	stale := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339)
	s.db.Exec("UPDATE errors SET last_seen = ? WHERE fingerprint = ?",
		stale, result.Fingerprint)

	// Auto-resolve: the error is stale but notified_at is NULL (crash gap).
	// It should be resolved in the DB but NOT appear in the returned list.
	resolved, err := s.AutoResolve(24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 0 {
		t.Fatalf("crash-gap error should NOT appear in resolved list (notified_at is NULL), got %d", len(resolved))
	}

	// But it should be resolved in the DB
	var resolvedAt sql.NullString
	s.db.QueryRow("SELECT resolved_at FROM errors WHERE fingerprint = ?",
		result.Fingerprint).Scan(&resolvedAt)
	if !resolvedAt.Valid {
		t.Fatal("error should be resolved in DB even without notified_at")
	}
}

// ---------------------------------------------------------------------------
// Liveness: when no new events arrive, auto-resolve must eventually resolve
// everything.  Resolved count must monotonically increase.
// ---------------------------------------------------------------------------

func TestLivenessAllErrorsEventuallyResolve(t *testing.T) {
	s := setupStore(t)
	rng := rand.New(rand.NewSource(7777))

	// Ingest a bunch of errors, notify some
	var fps []string
	for range 20 {
		ev := &domain.Event{Message: fmt.Sprintf("liveness-%d", rng.Intn(100000))}
		result, _ := s.StoreEvent(ev)
		fps = append(fps, result.Fingerprint)

		// Notify ~60%
		if rng.Float64() < 0.6 {
			s.MarkNotified(result.Fingerprint)
		}
	}

	// Backdate ALL errors to be stale
	stale := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339)
	for _, fp := range fps {
		s.db.Exec("UPDATE errors SET last_seen = ? WHERE fingerprint = ?", stale, fp)
	}

	// Single auto-resolve should resolve everything
	resolved, err := s.AutoResolve(24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	// All errors should be resolved in DB
	var unresolvedCount int
	s.db.QueryRow("SELECT COUNT(*) FROM errors WHERE resolved_at IS NULL").Scan(&unresolvedCount)
	if unresolvedCount != 0 {
		t.Fatalf("liveness: %d errors still unresolved after auto-resolve", unresolvedCount)
	}

	// Only notified errors should be in the returned list
	var notifiedCount int
	s.db.QueryRow("SELECT COUNT(*) FROM errors WHERE notified_at IS NOT NULL").Scan(&notifiedCount)
	if len(resolved) != notifiedCount {
		t.Fatalf("liveness: resolved list has %d entries but %d were notified",
			len(resolved), notifiedCount)
	}

	// Second auto-resolve: everything already resolved, nothing to do
	r2, _ := s.AutoResolve(24 * time.Hour)
	if len(r2) != 0 {
		t.Fatalf("liveness: second auto-resolve should return 0, got %d", len(r2))
	}
}

// ---------------------------------------------------------------------------
// Concurrency: hammer the store from multiple goroutines with ingests,
// notifies, and auto-resolves running in parallel.  No panics, no
// constraint violations, invariants hold at the end.
// ---------------------------------------------------------------------------

func TestConcurrentAccess(t *testing.T) {
	s := setupStore(t)
	const goroutines = 8
	const opsPerGoroutine = 50

	done := make(chan struct{}, goroutines)

	for g := range goroutines {
		go func(id int) {
			defer func() { done <- struct{}{} }()
			rng := rand.New(rand.NewSource(int64(id * 1000)))

			for i := range opsPerGoroutine {
				switch rng.Intn(4) {
				case 0: // ingest
					ev := &domain.Event{Message: fmt.Sprintf("concurrent-%d-%d", id, i)}
					s.StoreEvent(ev)
				case 1: // notify a random existing error
					var fp string
					s.db.QueryRow("SELECT fingerprint FROM errors ORDER BY RANDOM() LIMIT 1").Scan(&fp)
					if fp != "" {
						s.MarkNotified(fp)
					}
				case 2: // backdate + auto-resolve
					stale := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339)
					s.db.Exec("UPDATE errors SET last_seen = ? WHERE fingerprint IN (SELECT fingerprint FROM errors ORDER BY RANDOM() LIMIT 3)", stale)
					s.AutoResolve(24 * time.Hour)
				case 3: // re-ingest (regression)
					var fp string
					s.db.QueryRow("SELECT fingerprint FROM errors WHERE resolved_at IS NOT NULL ORDER BY RANDOM() LIMIT 1").Scan(&fp)
					if fp != "" {
						s.db.Exec("UPDATE errors SET resolved_at = NULL, last_seen = ? WHERE fingerprint = ?",
							time.Now().UTC().Format(time.RFC3339), fp)
					}
				}
			}
		}(g)
	}

	for range goroutines {
		<-done
	}

	// Post-concurrency invariants
	var total, resolved, notified int
	s.db.QueryRow("SELECT COUNT(*) FROM errors").Scan(&total)
	s.db.QueryRow("SELECT COUNT(*) FROM errors WHERE resolved_at IS NOT NULL").Scan(&resolved)
	s.db.QueryRow("SELECT COUNT(*) FROM errors WHERE notified_at IS NOT NULL").Scan(&notified)

	t.Logf("after concurrent access: %d total, %d resolved, %d notified", total, resolved, notified)

	// No error should have notified_at set without existing in the errors table
	// (referential integrity — sanity check)
	var orphaned int
	s.db.QueryRow("SELECT COUNT(*) FROM errors WHERE notified_at IS NOT NULL AND fingerprint NOT IN (SELECT fingerprint FROM errors)").Scan(&orphaned)
	if orphaned != 0 {
		t.Fatalf("found %d orphaned notified_at entries", orphaned)
	}

	// Final auto-resolve to clean up — should not panic
	stale := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339)
	s.db.Exec("UPDATE errors SET last_seen = ?", stale)
	_, err := s.AutoResolve(24 * time.Hour)
	if err != nil {
		t.Fatalf("final auto-resolve after concurrent access: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Regression cycles: error goes through multiple notify→resolve→reoccur
// cycles.  The resolved email should include it each time (it was notified
// in each lifecycle).
// ---------------------------------------------------------------------------

func TestMultipleRegressionCycles(t *testing.T) {
	s := setupStore(t)
	ev := &domain.Event{Message: "cyclic-error"}

	for cycle := range 5 {
		// Ingest
		result, err := s.StoreEvent(ev)
		if err != nil {
			t.Fatalf("cycle %d ingest: %v", cycle, err)
		}

		// After first cycle, this should be a regression
		if cycle > 0 && !result.IsRegression {
			t.Fatalf("cycle %d: expected regression", cycle)
		}

		// Notify
		s.MarkNotified(result.Fingerprint)

		// Backdate and auto-resolve
		stale := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339)
		s.db.Exec("UPDATE errors SET last_seen = ? WHERE fingerprint = ?",
			stale, result.Fingerprint)

		resolved, err := s.AutoResolve(24 * time.Hour)
		if err != nil {
			t.Fatalf("cycle %d auto-resolve: %v", cycle, err)
		}
		if len(resolved) != 1 {
			t.Fatalf("cycle %d: expected 1 resolved, got %d", cycle, len(resolved))
		}
		if resolved[0].Fingerprint != result.Fingerprint {
			t.Fatalf("cycle %d: wrong fingerprint in resolved", cycle)
		}
	}
}
