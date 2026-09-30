package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/PhilHem/drillip/internal/application/service"
	"github.com/PhilHem/drillip/internal/domain"
)

func TestDisplayedFingerprintsRemainActionableWhenPrefixesCollide(t *testing.T) {
	for _, listCommand := range []string{"list", "top", "recent"} {
		t.Run(listCommand, func(t *testing.T) {
			s := setupStore(t)
			app := service.New(s, nil, nil)
			c := &CLI{Errors: app, Correlation: app}
			messages := map[string]string{
				"c2a8398a3347b02d": "Fingerprint audit 96809",
				"c2a8398a15ea5f75": "Fingerprint audit 199543",
			}
			for fp, message := range messages {
				got, err := app.Ingest(&domain.Event{
					Message: message, Release: "v1", Tags: map[string]string{"service": "checkout"},
				})
				if err != nil || got != fp {
					t.Fatalf("ingest %q = %q, %v; want %s", message, got, err, fp)
				}
			}
			ctx := context.Background()
			if err := runCommand(c, "show", ctx, []string{"c2a8398a"}, io.Discard); !errors.Is(err, domain.ErrAmbiguousFingerprint) {
				t.Fatalf("shared prefix must remain ambiguous: %v", err)
			}
			run := func(command string, args ...string) string {
				t.Helper()
				var output bytes.Buffer
				if err := runCommand(c, command, ctx, args, &output); err != nil {
					t.Fatalf("%s %v: %v", command, args, err)
				}
				return output.String()
			}

			// Copy the actual first-column values a reader sees, then use them.
			listing := run(listCommand, "--tag", "service=checkout")
			copied := make(map[string]bool)
			for _, line := range strings.Split(listing, "\n") {
				fields := strings.Fields(line)
				if len(fields) == 0 || !domain.ValidFingerprint(fields[0]) {
					continue
				}
				fp := fields[0]
				if _, ok := messages[fp]; !ok {
					t.Fatalf("list supplied an incomplete or unexpected reference %q:\n%s", fp, listing)
				}
				copied[fp] = true
			}
			if len(copied) != len(messages) {
				t.Fatalf("list did not distinguish both groups:\n%s", listing)
			}

			hintPattern := regexp.MustCompile(`(?m)^→ drillip (show|trend|correlate) ([0-9a-f]+)$`)
			for fp := range copied {
				for _, command := range []string{"show", "trend", "correlate", "releases"} {
					output := run(command, fp)
					if !strings.Contains(output, fp) {
						t.Fatalf("%s did not identify the selected group %s:\n%s", command, fp, output)
					}
					if command == "show" && !strings.Contains(output, messages[fp]) {
						t.Fatalf("show returned the wrong group:\n%s", output)
					}
					for _, hint := range hintPattern.FindAllStringSubmatch(output, -1) {
						if hint[2] != fp {
							t.Fatalf("%s supplied a noncanonical hint %q", command, hint[0])
						}
						if result := run(hint[1], hint[2]); !strings.Contains(result, fp) {
							t.Fatalf("copied hint selected the wrong group:\n%s", result)
						}
					}
				}
			}

			selected := "c2a8398a3347b02d"
			if output := run("resolve", selected); !strings.Contains(output, "resolved "+selected) {
				t.Fatalf("resolution did not confirm the selected group:\n%s", output)
			}
			for fp := range copied {
				detail, err := app.GetDetail(ctx, fp)
				if err != nil || (detail.ResolvedAt != "") != (fp == selected) {
					t.Fatalf("resolution changed the wrong group %s: detail=%+v, err=%v", fp, detail, err)
				}
			}
		})
	}
}
