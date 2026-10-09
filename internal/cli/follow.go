package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// follow prints the events as they happen. It reconnects with Last-Event-ID
// when the connection drops, and stops when ctx ends.
func follow(ctx context.Context, env Env, g globals, req *request) error {
	lastID := req.query.Get("after")
	wait := 500 * time.Millisecond
	for ctx.Err() == nil {
		err := stream(ctx, env, g, req, &lastID)
		switch e := err.(type) { //nolint:errorlint // this package's own values
		case nil:
		case usageError:
			return e
		case apiError:
			return reportAPIError(env, g, e.rep)
		default:
			if ctx.Err() != nil {
				return nil
			}
			say(env.Stderr, "bosctl: the connection ended; trying again")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
		if wait < 5*time.Second {
			wait *= 2
		}
	}
	return nil
}

// stream reads one connection until it ends.
func stream(ctx context.Context, env Env, g globals, req *request, lastID *string) error {
	u := strings.TrimRight(g.server, "/") + "/api/v1" + req.path
	hr, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return usagef("the server address %q is not valid: %v", g.server, err)
	}
	hr.Header.Set("Accept", "text/event-stream")
	if g.token != "" {
		hr.Header.Set("Authorization", "Bearer "+g.token)
	}
	if *lastID != "" {
		hr.Header.Set("Last-Event-ID", *lastID)
	}
	res, err := env.Client.Do(hr)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }() // a stream that is being left
	if res.StatusCode != http.StatusOK {
		var body strings.Builder
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			body.WriteString(sc.Text())
		}
		return apiError{&reply{status: res.StatusCode, header: res.Header, body: []byte(body.String())}}
	}
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var id, typ, data string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if typ != "" {
				printStreamEvent(env, g, id, typ, data)
				*lastID = id
			}
			id, typ, data = "", "", ""
		case strings.HasPrefix(line, "id: "):
			id = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "event: "):
			typ = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		}
	}
	return sc.Err()
}

func printStreamEvent(env Env, g globals, id, typ, data string) {
	if typ == "gap" {
		say(env.Stderr, "events were missed: the log keeps only the latest")
		return
	}
	if g.json {
		sayf(env.Stdout, "{\"id\":%s,\"type\":%q,\"data\":%s}\n", orNull(id), typ, data)
		return
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		m = map[string]any{}
	}
	m["id"], m["type"] = id, typ
	say(env.Stdout, eventLine(m))
}

func orNull(s string) string {
	if s == "" {
		return "null"
	}
	return s
}
