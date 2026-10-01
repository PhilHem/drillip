package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/PhilHem/drillip/internal/adapter/httpwire"
	inport "github.com/PhilHem/drillip/internal/application/port/in"
	"github.com/PhilHem/drillip/internal/domain"
)

var _ inport.Backups = (*Client)(nil)

func (c *Client) Backup(ctx context.Context) (_ domain.DatabaseBackup, err error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	complete := false
	defer func() {
		if !complete {
			cancel()
		}
	}()
	capabilities, err := c.capabilities(ctx)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(capabilities.Features, httpwire.FeatureDatabaseBackup) {
		return nil, fmt.Errorf("server does not support drillip backup; upgrade the server together with the CLI")
	}
	target := *c.base
	target.Path = strings.TrimRight(target.Path, "/") + "/api/0/backup/"
	target.RawPath = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	// Backups stream to disk and can exceed the ordinary JSON request limits.
	client := *c.http
	client.Timeout = 2 * time.Minute
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download database backup: %w", err)
	}
	defer func() {
		if !complete {
			_ = resp.Body.Close()
		}
	}()
	if resp.StatusCode != http.StatusOK {
		var body struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body)
		if body.Error == "" {
			body.Error = http.StatusText(resp.StatusCode)
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, body.Error)
	}
	contentType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || contentType != "application/octet-stream" || resp.ContentLength < 100 {
		return nil, fmt.Errorf("invalid database backup response: expected a SQLite download with Content-Length")
	}
	header := make([]byte, 16)
	if _, err := io.ReadFull(resp.Body, header); err != nil {
		return nil, fmt.Errorf("read database backup header: %w", err)
	}
	if string(header) != "SQLite format 3\x00" {
		return nil, fmt.Errorf("invalid database backup response: missing SQLite header")
	}
	complete = true
	return &downloadedBackup{reader: io.MultiReader(bytes.NewReader(header), resp.Body), body: resp.Body, size: resp.ContentLength, cancel: cancel}, nil
}

type downloadedBackup struct {
	reader io.Reader
	body   io.ReadCloser
	size   int64
	cancel context.CancelFunc
}

func (b *downloadedBackup) Read(p []byte) (int, error) { return b.reader.Read(p) }
func (b *downloadedBackup) Size() int64                { return b.size }
func (b *downloadedBackup) Close() error {
	err := b.body.Close()
	b.cancel()
	return err
}
