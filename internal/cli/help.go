package cli

import (
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
)

// helpFor prints the overall usage, or that of a resource.
func helpFor(w io.Writer, rest []string) error {
	if len(rest) > 1 {
		if len(rest) > 2 {
			if cmd, _, ok := lookup(rest[1], rest[2:]); ok {
				return helpCommand(w, cmd)
			}
		}
		return helpResource(w, rest[1])
	}
	sayRaw(w, `bosctl drives a running bos server.

Usage:
  bosctl [--server URL] [--token TOKEN] [--json] <resource> <verb> [arguments] [flags]

The address and token can also be set with BOS_SERVER and BOS_TOKEN. The default
address is `+DefaultServer+`. With --json the answer is printed as the server sent it.

Resources:
`)
	var names []string
	seen := map[string]string{}
	for _, c := range commands {
		if _, ok := seen[c.Resource]; !ok {
			names = append(names, c.Resource)
			seen[c.Resource] = c.Summary
		}
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, n := range names {
		var verbs []string
		for _, c := range commands {
			if c.Resource == n && c.Verb != "" {
				verbs = append(verbs, c.Verb)
			}
		}
		sayf(tw, "  %s\t%s\n", n, strings.Join(verbs, ", "))
	}
	_ = tw.Flush() // a failed write to the terminal has nobody to be told to
	sayRaw(w, `
Run `+"`bosctl <resource> help`"+` for the verbs of a resource and `+"`bosctl help <resource> <verb>`"+` for one command.

Exit codes: 0 done, 1 the server answered with an error, 2 a mistake in the call,
3 the server could not be reached.
`)
	return nil
}

func helpResource(w io.Writer, resource string) error {
	found := false
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, c := range commands {
		if c.Resource != resource {
			continue
		}
		found = true
		sayf(tw, "bosctl %s %s\t%s\n", c.Resource, c.Verb, c.Summary)
	}
	_ = tw.Flush() // see helpFor
	if !found {
		return usagef("there is no resource %q; run `bosctl help`", resource)
	}
	return nil
}

func helpCommand(w io.Writer, c command) error {
	sayf(w, "%s\n\nUsage:\n  bosctl %s\n", c.Summary, usageLine(c))
	f := newFlags(c, Env{Getenv: func(string) string { return "" }}, globals{})
	var lines []string
	f.fs.VisitAll(func(fl *flag.Flag) {
		if fl.Name == "server" || fl.Name == "token" || fl.Name == "json" {
			return
		}
		lines = append(lines, fmt.Sprintf("  --%s\t%s", fl.Name, fl.Usage))
	})
	sort.Strings(lines)
	if len(lines) > 0 {
		say(w, "\nFlags:")
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		for _, l := range lines {
			say(tw, l)
		}
		_ = tw.Flush() // see helpFor
	}
	return nil
}

// usageLine is the way to call a command: bosctl server show ID [flags].
func usageLine(c command) string {
	parts := []string{c.Resource}
	if c.Verb != "" {
		parts = append(parts, c.Verb)
	}
	for _, a := range c.Args {
		parts = append(parts, strings.ToUpper(a))
	}
	if c.Special == "code" {
		parts = append(parts, "[COMMAND]")
	}
	parts = append(parts, "[flags]")
	return strings.Join(parts, " ")
}

// Reference writes the reference of every command in Markdown. docs/cli.md is
// this text, and a test fails when the file is out of date.
func Reference(w io.Writer) {
	sayRaw(w, "# bosctl reference\n\n")
	sayRaw(w, "This file is generated from the command table of `internal/cli`; do not edit it by hand. `go test ./internal/cli -update` rewrites it.\n\n")
	sayRaw(w, "```\nbosctl [--server URL] [--token TOKEN] [--json] <resource> <verb> [arguments] [flags]\n```\n\n")
	sayf(w, "`--server` (`BOS_SERVER`, default `%s`), `--token` (`BOS_TOKEN`) and `--json` can be given before or after the command. Exit codes: 0 done, 1 the server answered with an error, 2 a mistake in the call, 3 the server could not be reached.\n\n", DefaultServer)
	last := ""
	for _, c := range commands {
		if c.Resource != last {
			sayf(w, "## %s\n\n", c.Resource)
			last = c.Resource
		}
		sayf(w, "### `bosctl %s`\n\n%s.\n\n", usageLine(c), strings.TrimSuffix(c.Summary, "."))
		sayf(w, "Calls `%s /api/v1%s`.\n\n", c.Method, c.Path)
		f := newFlags(c, Env{Getenv: func(string) string { return "" }}, globals{})
		var rows []string
		f.fs.VisitAll(func(fl *flag.Flag) {
			if fl.Name == "server" || fl.Name == "token" || fl.Name == "json" {
				return
			}
			rows = append(rows, fmt.Sprintf("| `--%s` | %s |", fl.Name, fl.Usage))
		})
		sort.Strings(rows)
		if len(rows) > 0 {
			sayRaw(w, "| Flag | Meaning |\n|---|---|\n")
			for _, r := range rows {
				say(w, r)
			}
			say(w)
		}
	}
}
