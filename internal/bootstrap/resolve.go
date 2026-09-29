package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/PhilHem/drillip/internal/domain"
)

// resolveOnServer uses the same application operation and notification policy as
// HTTP clients. A failed request never falls back to a local database mutation.
func resolveOnServer(ctx context.Context, addr, fp string) (domain.ResolveResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL(addr)+"/api/0/resolve/"+fp+"/", nil)
	if err != nil {
		return domain.ResolveResult{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return domain.ResolveResult{}, fmt.Errorf("resolve request: %w", err)
	}
	defer resp.Body.Close()
	var body struct {
		Fingerprint string `json:"fingerprint"`
		ResolvedAt  string `json:"resolved_at"`
		Error       string `json:"error"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body)
	if resp.StatusCode != http.StatusOK {
		if body.Error == "" {
			body.Error = http.StatusText(resp.StatusCode)
		}
		return domain.ResolveResult{}, fmt.Errorf("resolve: HTTP %d: %s", resp.StatusCode, body.Error)
	}
	if decodeErr != nil {
		return domain.ResolveResult{}, fmt.Errorf("resolve response: %w", decodeErr)
	}
	if !domain.ValidFingerprint(body.Fingerprint) || len(body.Fingerprint) != 16 || body.ResolvedAt == "" {
		return domain.ResolveResult{}, fmt.Errorf("invalid resolve response")
	}
	return domain.ResolveResult{Matched: 1, Fingerprint: body.Fingerprint, ResolvedAt: body.ResolvedAt}, nil
}
