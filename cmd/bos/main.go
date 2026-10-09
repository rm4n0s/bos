// Command bos runs the Bowl of Scenes server.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	if err := runMain(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "bos:", err)
		os.Exit(1)
	}
}

func runMain(args []string) error {
	if len(args) > 0 && args[0] == "harness" {
		return runHarnessMain(args[1:])
	}
	fs := flag.NewFlagSet("bos", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to the JSON configuration file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := serve(ctx, *configPath, os.Getenv, logger, nil); err != nil {
		logError(logger, err)
		return err
	}
	return nil
}

// runHarnessMain runs the temporary harness of Milestone A (harness.go).
func runHarnessMain(args []string) error {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runHarness(ctx, args, os.Getenv, logger, os.Stdout); err != nil {
		logError(logger, err)
		return err
	}
	return nil
}
