package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"
	"unicode/utf8"
)

// lookupPath reads a dotted path out of a decoded JSON object.
func lookupPath(obj map[string]any, path string) any {
	var cur any = obj
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[part]
	}
	return cur
}

// text writes a JSON value as one short piece of text.
func text(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.Join(strings.Fields(x), " ")
	case bool:
		if x {
			return "yes"
		}
		return "no"
	case json.Number:
		return x.String()
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = text(e)
		}
		return strings.Join(parts, ",")
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func clip(s string, n int) string {
	if n <= 0 || utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// renderList prints a page of items as a table.
func renderList(env Env, cmd command, value any) {
	var items []any
	var page, pages, total int
	switch v := value.(type) {
	case map[string]any:
		items, _ = v["items"].([]any)
		page, pages, total = intOf(v["page"]), intOf(v["total_pages"]), intOf(v["total"])
	}
	if len(items) == 0 {
		say(env.Stdout, "(nothing)")
		return
	}
	w := tabwriter.NewWriter(env.Stdout, 0, 4, 2, ' ', 0)
	heads := make([]string, len(cmd.Columns))
	for i, c := range cmd.Columns {
		heads[i] = c.Header
	}
	say(w, strings.Join(heads, "\t"))
	for _, it := range items {
		obj, _ := it.(map[string]any)
		cells := make([]string, len(cmd.Columns))
		for i, c := range cmd.Columns {
			if c.Key == "progress" {
				cells[i] = fmt.Sprintf("%d/%d", intOf(obj["finished_jobs"]), intOf(obj["total_jobs"]))
				continue
			}
			cells[i] = clip(text(lookupPath(obj, c.Key)), c.Max)
		}
		say(w, strings.Join(cells, "\t"))
	}
	_ = w.Flush() // a failed write to the terminal has nobody to be told to
	if pages > 1 {
		sayf(env.Stderr, "page %d of %d (%d in all)\n", page, pages, total)
	}
}

func intOf(v any) int {
	switch x := v.(type) {
	case json.Number:
		n, _ := x.Int64()
		return int(n)
	case float64:
		return int(x)
	}
	return 0
}

// renderOne prints one resource as `key: value` lines. Long values are cut.
func renderOne(env Env, value any) {
	obj, ok := value.(map[string]any)
	if !ok {
		pretty, _ := json.MarshalIndent(value, "", "  ")
		say(env.Stdout, string(pretty))
		return
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if (keys[i] == "id") != (keys[j] == "id") {
			return keys[i] == "id"
		}
		return keys[i] < keys[j]
	})
	w := tabwriter.NewWriter(env.Stdout, 0, 4, 2, ' ', 0)
	for _, k := range keys {
		v := obj[k]
		var s string
		switch x := v.(type) {
		case map[string]any:
			b, _ := json.Marshal(x)
			s = shorten(string(b))
		case []any:
			b, _ := json.Marshal(x)
			s = shorten(string(b))
		default:
			s = shorten(text(v))
		}
		sayf(w, "%s:\t%s\n", k, s)
	}
	_ = w.Flush() // see renderList
}

// shorten cuts long values: a workflow is not something to read in a terminal.
func shorten(s string) string {
	if utf8.RuneCountInString(s) <= 100 {
		return s
	}
	return fmt.Sprintf("%s… (%d characters; use --json for all of it)", string([]rune(s)[:80]), utf8.RuneCountInString(s))
}

// renderPreview prints the result of a preview for a person.
func renderPreview(env Env, value any) {
	obj, _ := value.(map[string]any)
	if valid, _ := obj["valid"].(bool); !valid {
		say(env.Stdout, "The command is not valid:")
		list, _ := obj["errors"].([]any)
		for _, e := range list {
			m, _ := e.(map[string]any)
			pos, _ := m["position"].(map[string]any)
			sayf(env.Stdout, "  %s at %d: %s\n", text(m["rule"]), intOf(pos["offset"]), text(m["message"]))
		}
		return
	}
	say(env.Stdout, "The command is valid.")
	sayf(env.Stdout, "  combinations: %d\n  after where:  %d\n  kept:         %d\n  jobs:         %d\n",
		intOf(obj["combinations"]), intOf(obj["after_where"]), intOf(obj["kept"]), intOf(obj["jobs"]))
	if dyn, _ := obj["dynamic"].(bool); dyn {
		say(env.Stdout, "  the servers share the jobs as they become free")
	}
	if per, _ := obj["per_server"].([]any); len(per) > 0 {
		for _, p := range per {
			m, _ := p.(map[string]any)
			sayf(env.Stdout, "  %s: %d jobs\n", text(m["server"]), intOf(m["jobs"]))
		}
	}
	if warns, _ := obj["warnings"].([]any); len(warns) > 0 {
		for _, w := range warns {
			m, _ := w.(map[string]any)
			sayf(env.Stdout, "  warning: %s\n", text(m["message"]))
		}
	}
}

// renderEvents prints the event log as a table.
func renderEvents(env Env, value any) {
	obj, _ := value.(map[string]any)
	list, _ := obj["events"].([]any)
	if len(list) == 0 {
		say(env.Stdout, "(no events)")
		return
	}
	for _, e := range list {
		say(env.Stdout, eventLine(e.(map[string]any)))
	}
	if gap, _ := obj["gap"].(bool); gap {
		say(env.Stderr, "events were missed: the log keeps only the latest")
	}
}

// eventLine is one event on one line.
func eventLine(e map[string]any) string {
	parts := []string{text(e["id"]), text(e["type"])}
	for _, k := range []string{"job_id", "command_id", "project_id", "group_id"} {
		if n := intOf(e[k]); n != 0 && n != -1 {
			parts = append(parts, strings.TrimSuffix(k, "_id")+"="+fmt.Sprint(n))
		}
	}
	if s := text(e["server"]); s != "" {
		parts = append(parts, "server="+s)
	}
	if m := text(e["message"]); m != "" {
		parts = append(parts, m)
	}
	return strings.Join(parts, " ")
}
