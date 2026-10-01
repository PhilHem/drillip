package main

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/PhilHem/drillip/internal/bootstrap"
)

type inputReaderFunc func([]byte) (int, error)

func (read inputReaderFunc) Read(p []byte) (int, error) { return read(p) }

func TestProcessInputDoesNotReadForHelpOrInvalidInvocation(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"restore", "--input", "-"}} {
		t.Run(args[0], func(t *testing.T) {
			t.Setenv("DRILLIP_DB", "")
			input := newProcessInput(context.Background(), inputReaderFunc(func([]byte) (int, error) {
				t.Error("read input before a valid restore")
				return 0, io.EOF
			}))
			defer input.Close()
			_ = bootstrap.Run(context.Background(), args, input, io.Discard, io.Discard)
		})
	}
}

func TestProcessInputCancellationUnblocksPendingRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reading := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	input := newProcessInput(ctx, inputReaderFunc(func([]byte) (int, error) {
		close(reading)
		<-release
		return 0, io.EOF
	}))
	defer input.Close()
	done := make(chan error, 1)
	go func() { _, err := input.Read(make([]byte, 1)); done <- err }()
	<-reading
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not interrupt input")
	}
}
