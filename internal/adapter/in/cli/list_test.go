package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	inport "github.com/PhilHem/drillip/internal/application/port/in"
	"github.com/PhilHem/drillip/internal/domain"
)

type listBackend struct {
	inport.Errors
	query domain.ListQuery
	page  domain.ErrorPage
	err   error
}

func (b *listBackend) List(_ context.Context, query domain.ListQuery) (domain.ErrorPage, error) {
	b.query = query
	return b.page, b.err
}

func TestListOptionsReachUseCase(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want domain.ListQuery
	}{
		{"defaults", nil, domain.ListQuery{Sort: domain.SortLastSeen, Limit: 50}},
		{"options", []string{"--search", "  full %_ message  ", "--sort", "count", "--limit", "500", "--offset", "12", "--level", "warning", "--tag", "service=api=v2"},
			domain.ListQuery{Search: "  full %_ message  ", Sort: domain.SortCount, Limit: 500, Offset: 12,
				Filter: domain.ListFilter{Level: "warning", TagKey: "service", TagVal: "api=v2"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &listBackend{}
			if err := runCommand(&CLI{Errors: backend}, "list", context.Background(), tc.args, io.Discard); err != nil {
				t.Fatal(err)
			}
			if backend.query != tc.want {
				t.Fatalf("query = %+v, want %+v", backend.query, tc.want)
			}
		})
	}
}

func TestListInvalidArgumentsAndHelpNeedNoBackend(t *testing.T) {
	for _, args := range [][]string{
		{"--limit", "0"}, {"--limit", "501"}, {"--limit", "-1"}, {"--limit", "bad"},
		{"--offset", "-1"}, {"--offset", "bad"}, {"--sort", "first_seen"}, {"--sort", "COUNT"},
		{"--tag", "invalid"}, {"--tag", "key="}, {"--unknown"}, {"extra"}, {"--search"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var output bytes.Buffer
			if err := runCommand(&CLI{}, "list", context.Background(), args, &output); err == nil {
				t.Fatal("accepted invalid arguments")
			}
			if output.Len() != 0 {
				t.Fatalf("invalid arguments wrote stdout: %s", &output)
			}
		})
	}
	var help bytes.Buffer
	if err := runCommand(&CLI{}, "list", context.Background(), []string{"--help"}, &help); !errors.Is(err, flag.ErrHelp) {
		t.Fatal(err)
	}
	for _, want := range []string{"resolved and unresolved", "last-seen", "literal substring", "ASCII case-insensitive", "-limit", "-offset", "-sort", "-search", "-level", "-tag"} {
		if !strings.Contains(help.String(), want) {
			t.Errorf("missing %q from help: %s", want, &help)
		}
	}
}

func TestListDisplaysFullFingerprintsAndPageHint(t *testing.T) {
	search := "O'Reilly; $(printf injected) `printf injected` %_ \\\"\nnext"
	level := "warning; printf injected"
	tag := "key'=$(printf injected)& space"
	for _, prefix := range [][]string{
		nil,
		{"drillip", "--server", "http://example.test:8300/prefix?key='quoted'&v=1"},
	} {
		t.Run(strings.Join(prefix, " "), func(t *testing.T) {
			backend := &listBackend{page: domain.ErrorPage{Errors: []domain.ErrorSummary{
				{Fingerprint: "abcd111122223333", Level: "error", State: "resolved", Type: "First", Value: "message", LastSeen: time.Now().UTC().Format(time.RFC3339)},
				{Fingerprint: "abcd444455556666", Level: "warning", State: "open", Type: "Second", LastSeen: time.Now().UTC().Format(time.RFC3339)},
			}, HasMore: true}}
			var output bytes.Buffer
			args := []string{"--search", search, "--level", level, "--tag", tag, "--sort", "count", "--limit", "3", "--offset", "7"}
			if err := runCommand(&CLI{Errors: backend, CommandPrefix: prefix}, "list", context.Background(), args, &output); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"abcd111122223333", "abcd444455556666", "FINGERPRINT", "STATE", "LAST SEEN", "resolved", "show <fingerprint>"} {
				if !strings.Contains(output.String(), want) {
					t.Errorf("missing %q: %s", want, &output)
				}
			}
			// Capture real shell argument parsing, including embedded quotes and
			// newlines. Any expansion or extra command changes the captured bytes.
			_, hint, ok := strings.Cut(output.String(), " show <fingerprint>\n→ ")
			if !ok {
				t.Fatalf("missing page hint: %s", &output)
			}
			hint = strings.TrimSuffix(hint, "\n")
			shellOutput, err := exec.Command("sh", "-c", "drillip() { printf '%s\\000' \"$@\"; }\n"+hint).Output()
			if err != nil {
				t.Fatalf("copy hint: %v\n%s", err, hint)
			}
			gotArgs := strings.Split(strings.TrimSuffix(string(shellOutput), "\x00"), "\x00")
			prefixArgs := []string(nil)
			if len(prefix) > 0 {
				prefixArgs = prefix[1:]
			}
			if len(gotArgs) < len(prefixArgs) || !reflect.DeepEqual(gotArgs[:len(prefixArgs)], append([]string{}, prefixArgs...)) {
				t.Fatalf("backend changed: args = %q, prefix = %q", gotArgs, prefixArgs)
			}
			command, err := Parse(gotArgs[len(prefixArgs):], io.Discard)
			if err != nil {
				t.Fatalf("parse copied hint: %v (args %q)", err, gotArgs)
			}
			want := backend.query
			want.Offset += len(backend.page.Errors)
			if command.listQuery != want {
				t.Fatalf("next query = %+v, want %+v", command.listQuery, want)
			}
		})
	}
}

func TestListExhaustionAndBackendErrors(t *testing.T) {
	for _, page := range []domain.ErrorPage{
		{},
		{Errors: []domain.ErrorSummary{{Fingerprint: "abcd111122223333"}}},
	} {
		var output bytes.Buffer
		if err := runCommand(&CLI{Errors: &listBackend{page: page}}, "list", context.Background(), nil, &output); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(output.String(), "drillip list") {
			t.Fatalf("exhausted page offered another page: %s", &output)
		}
		if len(page.Errors) == 0 && !strings.Contains(output.String(), "no matching errors on this page") {
			t.Fatalf("missing empty result: %s", &output)
		}
	}
	wantErr := errors.New("list unavailable")
	var output bytes.Buffer
	if err := runCommand(&CLI{Errors: &listBackend{err: wantErr}}, "list", context.Background(), nil, &output); !errors.Is(err, wantErr) || output.Len() != 0 {
		t.Fatalf("backend error = %v, output = %q", err, &output)
	}
}

func TestTopAndRecentKeepTheirSelectionContracts(t *testing.T) {
	s := setupStore(t)
	insertTestError(t, s, "aaaa111122223333", "OldRecurring", "old but active", "v1")
	insertTestError(t, s, "aaaa111122223333", "OldRecurring", "old but active", "v1")
	insertTestError(t, s, "bbbb111122223333", "NewGroup", "just appeared", "v1")
	if _, err := s.RawDB().Exec("UPDATE errors SET first_seen = ? WHERE fingerprint = ?", time.Now().Add(-48*time.Hour).UTC().Format(time.RFC3339), "aaaa111122223333"); err != nil {
		t.Fatal(err)
	}
	var top, recent bytes.Buffer
	if err := runCommand(testCLI(s), "top", context.Background(), []string{"--limit", "1"}, &top); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(top.String(), "OldRecurring") || strings.Contains(top.String(), "NewGroup") {
		t.Fatalf("top no longer ranks total count: %s", &top)
	}
	if err := runCommand(testCLI(s), "recent", context.Background(), nil, &recent); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(recent.String(), "NewGroup") || strings.Contains(recent.String(), "OldRecurring") {
		t.Fatalf("recent no longer selects first-seen groups: %s", &recent)
	}
	for command, description := range map[string]string{"top": "total occurrence count", "recent": "first seen"} {
		var help bytes.Buffer
		_, _ = Parse([]string{command, "--help"}, &help)
		if !strings.Contains(help.String(), description) {
			t.Fatalf("%s help omits %q: %s", command, description, &help)
		}
		if _, err := Parse([]string{command, "--search", "message"}, io.Discard); err == nil {
			t.Fatalf("%s unexpectedly accepts list-only search", command)
		}
	}
}
