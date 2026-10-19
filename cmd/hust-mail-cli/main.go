package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/hust-open-atom-club/hust-mail-cli/pkg/cmd/root"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	cmd := root.NewCmdRoot(os.Stdout, os.Stderr)
	if err := cmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "error:", err)
		return 1
	}
	return 0
}
