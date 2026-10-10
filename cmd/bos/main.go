// Command bos runs the Bowl of Scenes server.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/rm4n0s/bos/internal/config"
	"github.com/rm4n0s/errors"
)

func main() {
	if err := runMain(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "bos:", err)
		os.Exit(1)
	}
}

func runMain(args []string) error {
	fs := flag.NewFlagSet("bos", flag.ContinueOnError)
	dbPath := fs.String("db", "data/bos.db", "path of the database file; it is created, with default settings next to it, if it does not exist. The settings are edited in the web page")
	logLevel := fs.String("log-level", "", "debug, info, warn or error; overrides the log_level setting and BOS_LOG_LEVEL. At debug every request and every workflow sent to ComfyUI is logged to the console")
	if err := fs.Parse(args); err != nil {
		return err
	}

	level := &slog.LevelVar{} // set from the configuration once it is read
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	getenv := overrideEnv(os.Getenv, config.EnvPrefix+"LOG_LEVEL", *logLevel)
	if err := serveAndRestart(ctx, *dbPath, getenv, logger, nil, withLogLevel(level)); err != nil {
		logError(logger, err)
		return err
	}
	return nil
}

// serveAndRestart runs serve, and runs it again, reading the settings anew,
// each time the settings page asks for a restart.
func serveAndRestart(ctx context.Context, dbPath string, getenv func(string) string, logger *slog.Logger, ready func(net.Addr), opts ...appOption) error {
	for {
		err := serve(ctx, dbPath, getenv, logger, ready, opts...)
		if !errors.Is(err, errRestart) {
			return err
		}
		logger.Info("restarting")
	}
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
