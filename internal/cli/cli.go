// Package cli is bosctl: a command-line client of the REST API (SPEC §15.6). It
// is a client like any other: it does not open the database, import the
// services or read the server's database or settings.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Exit codes (SPEC §15.6).
const (
	ExitOK          = 0
	ExitServerError = 1 // the server answered with an error
	ExitUsage       = 2
	ExitUnreachable = 3
)

// DefaultServer is where bosctl looks for a server.
const DefaultServer = "http://127.0.0.1:8080"

// Env is everything bosctl touches outside itself, so a test can replace it.
type Env struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Getenv func(string) string
	Client *http.Client
	// Poll is how long `command run --wait` sleeps between looks; the default is
	// half a second.
	Poll time.Duration
}

// usageError is a mistake in how bosctl was called.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func usagef(format string, args ...any) error { return usageError{fmt.Sprintf(format, args...)} }

// failure is an error the server answered with.
type failure struct{ code int }

func (e failure) Error() string { return fmt.Sprintf("failed with exit code %d", e.code) }

// globals are the flags every command takes.
type globals struct {
	server string
	token  string
	json   bool
}

func (g *globals) register(fs *flag.FlagSet, env Env) {
	server := env.Getenv("BOS_SERVER")
	if server == "" {
		server = DefaultServer
	}
	fs.StringVar(&g.server, "server", server, "the address of the bos server (BOS_SERVER)")
	fs.StringVar(&g.token, "token", env.Getenv("BOS_TOKEN"), "the bearer token, if the server has one (BOS_TOKEN)")
	fs.BoolVar(&g.json, "json", false, "print the answer as JSON")
}

// Run runs bosctl and returns its exit code.
func Run(ctx context.Context, args []string, env Env) int {
	if env.Getenv == nil {
		env.Getenv = os.Getenv
	}
	if env.Client == nil {
		env.Client = http.DefaultClient
	}
	if env.Poll <= 0 {
		env.Poll = 500 * time.Millisecond
	}
	err := run(ctx, args, env)
	switch e := err.(type) { //nolint:errorlint // the errors of this package are returned as they are
	case nil:
		return ExitOK
	case usageError:
		say(env.Stderr, "bosctl:", e.msg)
		say(env.Stderr, "Run `bosctl help` for how to use it.")
		return ExitUsage
	case failure:
		return e.code
	}
	say(env.Stderr, "bosctl:", err)
	return ExitServerError
}

func run(ctx context.Context, args []string, env Env) error {
	var g globals
	top := flag.NewFlagSet("bosctl", flag.ContinueOnError)
	top.SetOutput(io.Discard)
	g.register(top, env)
	// Global flags come first; they are also accepted after the command.
	rest, err := parseLeading(top, args)
	if err != nil {
		return usagef("%v", err)
	}
	if len(rest) == 0 || rest[0] == "help" || rest[0] == "--help" || rest[0] == "-h" {
		return helpFor(env.Stdout, rest)
	}
	resource, tail := rest[0], rest[1:]
	if len(tail) > 0 && (tail[0] == "help" || tail[0] == "--help") {
		return helpResource(env.Stdout, resource)
	}
	cmd, tail, ok := lookup(resource, tail)
	if !ok {
		return usagef("there is no command %q; run `bosctl help`", strings.TrimSpace(resource+" "+firstOr(tail)))
	}
	return execute(ctx, env, g, cmd, tail)
}

func firstOr(s []string) string {
	if len(s) > 0 {
		return s[0]
	}
	return ""
}

// parseLeading parses the flags at the start of args and returns the rest.
func parseLeading(fs *flag.FlagSet, args []string) ([]string, error) {
	var rest []string
	for len(args) > 0 {
		if !strings.HasPrefix(args[0], "-") || args[0] == "-" {
			break
		}
		if args[0] == "-h" || args[0] == "--help" {
			return append(rest, args...), nil
		}
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
	}
	return append(rest, args...), nil
}

// lookup finds the command for a resource and the next word.
func lookup(resource string, tail []string) (command, []string, bool) {
	for _, c := range commands {
		if c.Resource != resource {
			continue
		}
		if c.Verb == "" {
			return c, tail, true
		}
		if len(tail) > 0 && tail[0] == c.Verb {
			return c, tail[1:], true
		}
	}
	return command{}, nil, false
}
