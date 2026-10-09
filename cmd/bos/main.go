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
	fs := flag.NewFlagSet("bos", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to the JSON configuration file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	level := &slog.LevelVar{} // set from the configuration once it is read
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := serve(ctx, *configPath, os.Getenv, logger, nil, withLogLevel(level)); err != nil {
		logError(logger, err)
		return err
	}
	return nil
}
