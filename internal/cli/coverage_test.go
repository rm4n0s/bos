package cli

import (
	"strings"
	"testing"

	"github.com/rm4n0s/bos/internal/api"
	"github.com/rm4n0s/bos/internal/apptest"
)

// TestEveryOperationOfTheAPIHasACommand is SPEC §15.6: an operation of the
// OpenAPI document with no command, and not on the list of exclusions, fails
// here; so does a command for an operation that does not exist.
func TestEveryOperationOfTheAPIHasACommand(t *testing.T) {
	env := apptest.New(t)
	a := api.New(api.Deps{Services: env.Services, Events: env.Events, Files: env.Files, Dirs: env.Dirs})
	spec := a.OpenAPI()
	paths := spec["paths"].(map[string]any)

	have := map[string]bool{}
	for _, c := range commands {
		have[c.Method+" "+c.Path] = true
	}
	api := map[string]bool{}
	for path, item := range paths {
		for method := range item.(map[string]any) {
			key := strings.ToUpper(method) + " " + path
			api[key] = true
			if !have[key] {
				if _, ok := excluded[key]; !ok {
					t.Errorf("%s has no bosctl command", key)
				}
			}
		}
	}
	for key := range have {
		if !api[key] {
			t.Errorf("a command calls %s, which the API does not have", key)
		}
	}
	for key, why := range excluded {
		if why == "" {
			t.Errorf("%s is excluded without a reason", key)
		}
		if have[key] {
			t.Errorf("%s is excluded and also has a command", key)
		}
	}
	// A command is reachable by one name only.
	seen := map[string]bool{}
	for _, c := range commands {
		name := c.Resource + " " + c.Verb
		if seen[name] {
			t.Errorf("two commands are called %q", name)
		}
		seen[name] = true
	}
}
