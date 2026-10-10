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

	"github.com/rm4n0s/bos/internal/config"
)

func main() {
	if err := runMain(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "bos:", err)
		os.Exit(1)
	}
}

func runMain(args []string) error {
	fs := flag.NewFlagSet("bos", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to the JSON configuration file; it is created if it does not exist")
	logLevel := fs.String("log-level", "", "debug, info, warn or error; overrides log_level of the file and BOS_LOG_LEVEL. At debug every request and every workflow sent to ComfyUI is logged to the console")
	if err := fs.Parse(args); err != nil {
		return err
	}

	level := &slog.LevelVar{} // set from the configuration once it is read
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	getenv := overrideEnv(os.Getenv, config.EnvPrefix+"LOG_LEVEL", *logLevel)
	if err := serve(ctx, *configPath, getenv, logger, nil, withLogLevel(level)); err != nil {
		logError(logger, err)
		return err
	}
	return nil
}

// overrideEnv returns getenv with one variable replaced, so a command-line flag
// wins over the environment and the file. An empty value changes nothing.
func overrideEnv(getenv func(string) string, name, value string) func(string) string {
	if value == "" {
		return getenv
	}
	return func(k string) string {
		if k == name {
			return value
		}
		return getenv(k)
	}
}
