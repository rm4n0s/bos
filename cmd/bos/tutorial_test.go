package main

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/bos/internal/cli"
	"github.com/rm4n0s/bos/internal/comfy/comfytest"
)

// tutorialStep is one bosctl command of docs/tutorial.md.
type tutorialStep struct {
	line int
	args []string
	exit int
}

var exitComment = regexp.MustCompile(`\s+#\s*exit\s+(\d+)\s*$`)

// tutorialSteps reads the blocks marked `bosctl` out of the tutorial.
func tutorialSteps(t *testing.T, doc string) []tutorialStep {
	t.Helper()
	b, err := os.ReadFile("../../docs/" + doc)
	if err != nil {
		t.Fatal(err)
	}
	var steps []tutorialStep
	in := false
	var pending string
	for i, line := range strings.Split(string(b), "\n") {
		switch {
		case strings.HasPrefix(line, "```bosctl"):
			in = true
		case in && strings.HasPrefix(line, "```"):
			in = false
		case in && strings.TrimSpace(line) != "":
			pending += strings.TrimSuffix(line, "\\")
			if strings.HasSuffix(line, "\\") {
				continue
			}
			exit := 0
			if m := exitComment.FindStringSubmatch(pending); m != nil {
				exit, _ = strconv.Atoi(m[1])
				pending = exitComment.ReplaceAllString(pending, "")
			}
			words, err := shellWords(pending)
			if err != nil {
				t.Fatalf("docs/%s:%d: %v", doc, i+1, err)
			}
			if len(words) == 0 || words[0] != "bosctl" {
				t.Fatalf("docs/%s:%d: a bosctl block must hold bosctl commands, not %q", doc, i+1, pending)
			}
			steps = append(steps, tutorialStep{line: i + 1, args: words[1:], exit: exit})
			pending = ""
		}
	}
	return steps
}

// shellWords splits a command line the way a shell does for the cases the
// tutorial uses: single quotes, double quotes and spaces.
func shellWords(s string) ([]string, error) {
	var words []string
	var cur strings.Builder
	started := false
	var quote rune
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, started = r, true
		case r == ' ' || r == '\t':
			if started || cur.Len() > 0 {
				words = append(words, cur.String())
				cur.Reset()
				started = false
			}
		default:
			cur.WriteRune(r)
		}
	}
	if quote != 0 {
		return nil, os.ErrInvalid
	}
	if started || cur.Len() > 0 {
		words = append(words, cur.String())
	}
	return words, nil
}

func TestShellWords(t *testing.T) {
	got, err := shellWords(`bosctl command add 1 'gpu1 -$ a: b * c' --name "two words" --x ''  y`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"bosctl", "command", "add", "1", "gpu1 -$ a: b * c", "--name", "two words", "--x", "", "y"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("words = %q", got)
	}
	if _, err := shellWords(`a 'b`); err == nil {
		t.Error("an open quote was accepted")
	}
}

// TestTutorial runs every bosctl command of the tutorial and of the guide for
// AI agents, in order, against the whole program and the stand-in ComfyUI. The
// documents cannot say something that does not work.
func TestTutorial(t *testing.T) {
	tests := []struct {
		doc      string
		minSteps int
		download string
		// output maps the start of a command to text its standard output must hold.
		output map[string][]string
	}{
		{doc: "tutorial.md", minSteps: 25, download: "tutorial-results.zip"},
		{doc: "agents.md", minSteps: 15, download: "agent-results.zip", output: map[string][]string{
			"command preview gpu1 -$ anime: characters": {`"valid":true`, `"jobs":2`},
			"command preview gpu1 -$ anime: nobody":     {`"valid":false`, `"tag":"GroupNotFound"`, `"offset":15`},
			"command add 1 gpu1 -$ anime: nobody":       {`"tag":"CommandValidationFailed"`, `"metadata":{"errors":[`},
			"command run 1":                             {"finished: 2 finished of 2 jobs"},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.doc, func(t *testing.T) {
			steps := tutorialSteps(t, tc.doc)
			if len(steps) < tc.minSteps {
				t.Fatalf("only %d commands found in %s", len(steps), tc.doc)
			}
			fake := comfytest.New(30 * time.Millisecond)
			t.Cleanup(fake.Close)
			p := startProgram(t, testEnv(t, nil))
			t.Chdir("../..") // the paths in the documents start at the root of the project
			t.Cleanup(func() { _ = os.Remove(tc.download) })

			for _, s := range steps {
				args := make([]string, len(s.args))
				for i, a := range s.args {
					args[i] = strings.ReplaceAll(a, "http://127.0.0.1:8188", fake.URL)
				}
				var out, errOut bytes.Buffer
				code := cli.Run(t.Context(), args, cli.Env{
					Stdout: &out, Stderr: &errOut, Poll: 10 * time.Millisecond,
					Getenv: func(k string) string {
						if k == "BOS_SERVER" {
							return p.base
						}
						return ""
					},
				})
				where := fmt.Sprintf("docs/%s:%d: bosctl %s", tc.doc, s.line, strings.Join(s.args, " "))
				if code != s.exit {
					t.Fatalf("%s\nexit %d, the document says %d\n%s%s", where, code, s.exit, out.String(), errOut.String())
				}
				words := slices.DeleteFunc(slices.Clone(args), func(a string) bool { return a == "--json" })
				if s.exit == 0 && len(words) > 1 && words[0] == "command" && words[1] == "run" && !strings.Contains(out.String(), "finished: ") {
					t.Fatalf("%s\nthe command did not finish:\n%s", where, out.String())
				}
				for start, wants := range tc.output {
					if !strings.HasPrefix(strings.Join(words, " "), start) {
						continue
					}
					for _, w := range wants {
						if !strings.Contains(out.String(), w) {
							t.Errorf("%s\nthe output lacks %s:\n%s", where, w, out.String())
						}
					}
				}
			}
			if _, err := os.Stat(tc.download); err != nil {
				t.Errorf("the download left no file: %v", err)
			}
		})
	}
}
