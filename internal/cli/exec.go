package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// execute runs one command.
func execute(ctx context.Context, env Env, base globals, cmd command, args []string) error {
	f := newFlags(cmd, env, base)
	pos, err := f.parse(args)
	if err != nil {
		return err
	}
	g := f.g
	if g.server == "" {
		return usagef("no server address: use --server or BOS_SERVER")
	}
	var timeout time.Duration
	if f.timeout != nil {
		if timeout, err = parseTimeout(*f.timeout); err != nil {
			return err
		}
	}
	req, err := buildRequest(ctx, env, g, cmd, f, pos)
	if err != nil {
		return asReported(env, g, err)
	}
	if cmd.Special == "follow" && *f.follow {
		return follow(ctx, env, g, req)
	}
	rep, err := send(ctx, env, g, req)
	if err != nil {
		return asReported(env, g, err)
	}
	if rep.status < 200 || rep.status > 299 {
		return reportAPIError(env, g, rep)
	}
	if cmd.Special == "wait" && *f.wait {
		if err := show(env, g, cmd, rep, f); err != nil {
			return err
		}
		return waitFor(ctx, env, g, pos[0], timeout)
	}
	return show(env, g, cmd, rep, f)
}

// asReported prints an error that the lower layers return and picks the exit
// code: an unreachable server is 3, an error answer is 1, a mistake in the
// call is 2.
func asReported(env Env, g globals, err error) error {
	switch e := err.(type) { //nolint:errorlint // these are this package's own values
	case usageError:
		return e
	case unreachable:
		say(env.Stderr, "bosctl:", e.error)
		return failure{ExitUnreachable}
	case apiError:
		return reportAPIError(env, g, e.rep)
	}
	return err
}

func parseTimeout(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, usagef("--timeout is a duration such as 30m, not %q", s)
	}
	return d, nil
}

// reportAPIError prints the error envelope of an error answer.
func reportAPIError(env Env, g globals, rep *reply) error {
	if g.json {
		say(env.Stdout, strings.TrimSpace(string(rep.body)))
		return failure{ExitServerError}
	}
	var envlp struct {
		Error struct {
			Tag      string         `json:"tag"`
			Message  string         `json:"message"`
			Metadata map[string]any `json:"metadata"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rep.body, &envlp); err != nil || envlp.Error.Tag == "" {
		sayf(env.Stderr, "bosctl: the server answered %d: %s\n", rep.status, strings.TrimSpace(truncate(string(rep.body), 300)))
		return failure{ExitServerError}
	}
	sayf(env.Stderr, "%s: %s\n", envlp.Error.Tag, envlp.Error.Message)
	if list, ok := envlp.Error.Metadata["errors"].([]any); ok {
		for _, e := range list {
			m, _ := e.(map[string]any)
			line := "  "
			if rule, _ := m["rule"].(string); rule != "" {
				line += rule + " "
			}
			if pos, ok := m["position"].(map[string]any); ok {
				if off, ok := pos["offset"].(float64); ok {
					line += fmt.Sprintf("at %d ", int(off))
				}
			}
			msg, _ := m["message"].(string)
			say(env.Stderr, strings.TrimRight(line, " ")+": "+msg)
		}
	}
	return failure{ExitServerError}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// show prints the answer of a successful request.
func show(env Env, g globals, cmd command, rep *reply, f *flags) error {
	switch cmd.Result {
	case resultBytes:
		return saveFile(env, g, cmd, rep, f)
	case resultNone:
		if !g.json {
			say(env.Stderr, "done")
		}
		return nil
	}
	if g.json {
		say(env.Stdout, strings.TrimRight(string(rep.body), "\n"))
		return nil
	}
	var value any
	d := json.NewDecoder(strings.NewReader(string(rep.body)))
	d.UseNumber()
	if err := d.Decode(&value); err != nil {
		say(env.Stdout, strings.TrimRight(string(rep.body), "\n"))
		return nil
	}
	switch {
	case cmd.Verb == "preview":
		renderPreview(env, value)
	case cmd.Result == resultList:
		renderList(env, cmd, value)
	case cmd.Result == resultOne:
		renderOne(env, value)
	case cmd.Resource == "events":
		renderEvents(env, value)
	default:
		pretty, _ := json.MarshalIndent(value, "", "  ")
		say(env.Stdout, string(pretty))
	}
	return nil
}

// saveFile writes a downloaded file to --output, or to a name made from the request.
func saveFile(env Env, g globals, cmd command, rep *reply, f *flags) error {
	out := *f.output
	if out == "-" {
		_, err := env.Stdout.Write(rep.body)
		return err
	}
	if out == "" {
		out = defaultOutput(cmd, f, rep)
	}
	if err := os.WriteFile(out, rep.body, 0o600); err != nil {
		return wrapf(err, "cannot write %s", out)
	}
	if !g.json {
		sayf(env.Stderr, "saved %s (%d bytes)\n", out, len(rep.body))
	}
	return nil
}

func defaultOutput(cmd command, f *flags, rep *reply) string {
	if cmd.Verb == "download" {
		if cd := rep.header.Get("Content-Disposition"); strings.Contains(cd, `filename="`) {
			name := cd[strings.Index(cd, `filename="`)+len(`filename="`):]
			if i := strings.Index(name, `"`); i > 0 {
				return filepath.Base(name[:i])
			}
		}
		return "download.zip"
	}
	if args := f.fs.Args(); len(args) > 0 {
		return filepath.Base(args[len(args)-1])
	}
	return "file"
}

// statusOK is the code of a plain success.
const statusOK = http.StatusOK

// waitFor follows a command until it ends. It returns nil when every job
// finished, and a failure when any ended in error or the command was stopped.
func waitFor(ctx context.Context, env Env, g globals, id string, timeout time.Duration) error {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	last := ""
	for {
		status, summary, err := commandProgress(ctx, env, g, id)
		if err != nil {
			if ctx.Err() != nil {
				say(env.Stderr, "bosctl: stopped waiting")
				return failure{ExitServerError}
			}
			return asReported(env, g, err)
		}
		if key := status + ": " + summary; key != last {
			say(env.Stdout, key)
			last = key
		}
		switch status {
		case "finished":
			return nil
		case "error":
			sayf(env.Stderr, "bosctl: the command ended with errors; see `bosctl job list %s --status error`\n", id)
			return failure{ExitServerError}
		case "idle":
			say(env.Stderr, "bosctl: the command is idle: it was stopped, or has nothing to run")
			return failure{ExitServerError}
		}
		select {
		case <-ctx.Done():
			say(env.Stderr, "bosctl: stopped waiting")
			return failure{ExitServerError}
		case <-time.After(env.Poll):
		}
	}
}

// commandProgress reads the status of a command and a line that counts its
// jobs by status.
func commandProgress(ctx context.Context, env Env, g globals, id string) (status, summary string, err error) {
	rep, err := send(ctx, env, g, &request{method: "GET", path: "/commands/" + id})
	if err != nil {
		return "", "", err
	}
	if rep.status != statusOK {
		return "", "", apiError{rep}
	}
	var c struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rep.body, &c); err != nil {
		return "", "", err
	}
	counts := map[string]int{}
	total := 0
	for page := 1; ; page++ {
		rep, err := send(ctx, env, g, &request{method: "GET", path: "/commands/" + id + "/jobs", query: map[string][]string{"page": {strconv.Itoa(page)}, "page_size": {"200"}}})
		if err != nil {
			return "", "", err
		}
		if rep.status != statusOK {
			return "", "", apiError{rep}
		}
		var jobs struct {
			Items []struct {
				Status string `json:"status"`
			} `json:"items"`
			TotalPages int `json:"total_pages"`
		}
		if err := json.Unmarshal(rep.body, &jobs); err != nil {
			return "", "", err
		}
		for _, j := range jobs.Items {
			counts[j.Status]++
			total++
		}
		if page >= jobs.TotalPages {
			break
		}
	}
	var parts []string
	for _, st := range []string{"idle", "queued", "processing", "error", "finished"} {
		if counts[st] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[st], st))
		}
	}
	return c.Status, fmt.Sprintf("%s of %d jobs", strings.Join(parts, ", "), total), nil
}
