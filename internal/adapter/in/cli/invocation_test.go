package cli

import (
	"context"
	"io"
)

func runCommand(c *CLI, name string, ctx context.Context, args []string, w io.Writer) error {
	cmd, err := Parse(append([]string{name}, args...), w)
	if err != nil {
		return err
	}
	return cmd.Run(ctx, c, w)
}
func commandRunner(c *CLI, name string) func(context.Context, []string, io.Writer) error {
	return func(ctx context.Context, args []string, w io.Writer) error { return runCommand(c, name, ctx, args, w) }
}
