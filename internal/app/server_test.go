package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunServeStartupErrors(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	for _, tc := range []struct {
		name string
		cfg  config
		want string
	}{
		{"database", config{DB: filepath.Join(t.TempDir(), "missing", "errors.db"), Addr: "127.0.0.1:0"}, "init db:"},
		{"occupied address", config{DB: filepath.Join(t.TempDir(), "errors.db"), Addr: listener.Addr().String()}, "listen:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := runServe(context.Background(), tc.cfg); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestRunServeCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() {
		done <- runServe(ctx, config{DB: ":memory:", Addr: "127.0.0.1:0"})
	}()
	waitServer(t, done)
}

func TestServeHTTPWaitsForActiveRequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})}
	defer srv.Close()
	shutdown := make(chan struct{})
	srv.RegisterOnShutdown(func() { close(shutdown) })
	serverDone := make(chan error, 1)
	go func() { serverDone <- serveHTTP(ctx, srv, listener) }()
	requestDone := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get("http://" + listener.Addr().String())
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent {
				err = errors.New("request did not finish normally")
			}
		}
		requestDone <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case <-shutdown:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not start")
	}
	select {
	case err := <-serverDone:
		t.Fatalf("server returned before the request finished: %v", err)
	default:
	}
	close(release)
	waitServer(t, requestDone)
	waitServer(t, serverDone)
}

func TestServeHTTPReturnsListenerError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener.Close()
	done := make(chan error, 1)
	go func() { done <- serveHTTP(context.Background(), &http.Server{}, listener) }()
	select {
	case err := <-done:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("error = %v, want closed listener", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not return after listener failure")
	}
}

func waitServer(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server operation did not finish")
	}
}
