package main

import (
	"context"
	"io"
)

// processInput makes process-owned stdin interruptible without changing its
// descriptor flags. Standard input can block even when os.Stdin.Close is called.
// A single pump starts on the first read, so help and other commands consume
// no input. If the source blocks after cancellation, it lasts until process exit.
type processInput struct {
	ctx    context.Context
	source io.Reader
	pipe   *io.PipeReader
	stop   func() bool
}

func newProcessInput(ctx context.Context, source io.Reader) *processInput {
	return &processInput{ctx: ctx, source: source}
}

func (input *processInput) Read(p []byte) (int, error) {
	if err := input.ctx.Err(); err != nil {
		return 0, err
	}
	if input.pipe == nil {
		reader, writer := io.Pipe()
		input.pipe = reader
		input.stop = context.AfterFunc(input.ctx, func() { _ = writer.CloseWithError(input.ctx.Err()) })
		go func() {
			_, err := io.Copy(writer, input.source)
			_ = writer.CloseWithError(err)
		}()
	}
	return input.pipe.Read(p)
}

func (input *processInput) Close() error {
	if input.pipe == nil {
		return nil
	}
	input.stop()
	return input.pipe.Close()
}
