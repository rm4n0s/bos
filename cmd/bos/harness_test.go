package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/bos/internal/comfy/comfytest"
	"github.com/rm4n0s/errors"
)

// quick makes the scheduler and the adapter poll every few milliseconds.
func quick(t *appTuning) { t.probeInterval, t.pollInterval = 10*time.Millisecond, 10*time.Millisecond }

func workflowFile(t *testing.T, kind, name string) string {
	t.Helper()
	return filepath.Join("..", "..", "examples", "workflows", kind, name)
}

func TestHarnessRunsACommandEndToEnd(t *testing.T) {
	fake := comfytest.New(20 * time.Millisecond)
	t.Cleanup(fake.Close)
	var out bytes.Buffer
	err := runHarness(t.Context(), []string{
		"-server", fake.URL,
		"-workflow", workflowFile(t, "creators", "illustrious_xl.json"),
		"-prompts", "a red fox, a blue owl, a green frog",
		"-fixer-workflow", workflowFile(t, "fixers", "simple_upscale.json"),
		"-timeout", "30s",
	}, testEnv(t, nil), testLogger(t), &out, quick)
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	text := out.String()
	for _, want := range []string{"preview: valid=true jobs=3", "running command", "done: every job finished"} {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %q:\n%s", want, text)
		}
	}
	if n := strings.Count(text, ": finished attempts=0"); n != 3 {
		t.Errorf("%d finished jobs reported:\n%s", n, text)
	}
	if n := len(fake.Prompts()); n != 6 {
		t.Errorf("%d prompts, want 3 generations and 3 fixer steps", n)
	}
}

func TestHarnessReportsAFailingCommand(t *testing.T) {
	fake := comfytest.New(time.Millisecond)
	t.Cleanup(fake.Close)
	// The stand-in produces files for SaveImage nodes only, so a workflow that
	// ends in PreviewImage has no result and its jobs fail.
	preview := filepath.Join(t.TempDir(), "preview.json")
	if err := os.WriteFile(preview, []byte(`{"1":{"class_type":"CLIPTextEncode","inputs":{"text":""},"_meta":{"title":"Positive"}},`+
		`"2":{"class_type":"PreviewImage","inputs":{"images":["1",0]},"_meta":{"title":"Preview"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := runHarness(t.Context(), []string{"-server", fake.URL, "-workflow", preview, "-prompts", "x", "-timeout", "30s"},
		testEnv(t, nil), testLogger(t), &out, quick)
	appErr, ok := errors.FromError(err)
	if !ok || appErr.Tag != "CommandNotFinished" {
		t.Fatalf("error = %v, want CommandNotFinished\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "error: ComfyUIResultMissing") {
		t.Errorf("the job's error is not reported:\n%s", out.String())
	}
}

func TestHarnessServerThatNeverAnswers(t *testing.T) {
	fake := comfytest.New(time.Millisecond)
	t.Cleanup(fake.Close)
	fake.SetDown(true)
	var out bytes.Buffer
	err := runHarness(t.Context(), []string{"-server", fake.URL, "-workflow", workflowFile(t, "creators", "illustrious_xl.json"), "-timeout", "300ms"},
		testEnv(t, nil), testLogger(t), &out, quick)
	if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "ServerUnreachable" {
		t.Errorf("error = %v, want ServerUnreachable", err)
	}
}

func TestHarnessArguments(t *testing.T) {
	var out bytes.Buffer
	err := runHarness(t.Context(), nil, testEnv(t, nil), testLogger(t), &out, quick)
	if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "InvalidRequest" {
		t.Errorf("no workflow: %v", err)
	}
	err = runHarness(t.Context(), []string{"-workflow", "/no/such/file.json"}, testEnv(t, nil), testLogger(t), &out, quick)
	if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "FileStoreFailure" {
		t.Errorf("missing workflow file: %v", err)
	}
	err = runHarness(t.Context(), []string{"-nope"}, testEnv(t, nil), testLogger(t), &out, quick)
	if err == nil {
		t.Error("an unknown flag was accepted")
	}
}
