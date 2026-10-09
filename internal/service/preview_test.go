package service

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/bos/internal/command"
	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

func TestPreviewCountsAndStoresNothing(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	pv, err := e.Commands.Preview(ctx(t), "gpu1, gpu2 -$ anime: characters * poses where not #calm limit 4 spread even")
	check(t, err)
	if !pv.Valid || len(pv.Problems) != 0 {
		t.Fatalf("preview = %+v", pv)
	}
	want := command.Counts{Combinations: 9, AfterWhere: 6, Kept: 4, Jobs: 4}
	if pv.Counts != want || pv.Dynamic || !reflect.DeepEqual(pv.PerServer, []command.ServerCount{{Server: "gpu1", Jobs: 2}, {Server: "gpu2", Jobs: 2}}) {
		t.Errorf("counts = %+v, per server %+v, dynamic %v", pv.Counts, pv.PerServer, pv.Dynamic)
	}
	dyn, err := e.Commands.Preview(ctx(t), "gpu1 -$ anime: characters")
	check(t, err)
	if !dyn.Dynamic || dyn.Jobs != 3 {
		t.Errorf("dynamic preview = %+v", dyn)
	}

	if page, _ := e.Commands.List(ctx(t), w.project.ID, domain.Page{}); page.Total != 0 {
		t.Error("a preview stored a command")
	}
}

func TestPreviewReportsProblemsInTheResult(t *testing.T) {
	e := newEnv(t)
	e.world(t)
	tests := []struct {
		name string
		code string
		rule string
		n    int
	}{
		{"lexical error", "gpu1 -$ anime: characters @", "LEX", 1},
		{"syntax error", "gpu1 -$ anime characters", "SYN", 1},
		{"restriction", "gpu1 -$ anime: characters && poses || characters", "S1", 1},
		{"semantic errors are all listed", "gpu9 -$ nope: characters(zzz)", "SEM", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pv, err := e.Commands.Preview(ctx(t), tt.code)
			check(t, err)
			if pv.Valid || len(pv.Problems) != tt.n || pv.Problems[0].Rule != tt.rule {
				t.Errorf("preview = %+v", pv)
			}
			if pv.Warnings == nil || pv.PerServer == nil {
				t.Error("lists must be empty, not null, for the JSON form")
			}
		})
	}
}

func TestPreviewSoftLimit(t *testing.T) {
	e := newEnv(t, withLimits(Limits{PreviewSoftLimit: 5}))
	e.world(t)
	pv, err := e.Commands.Preview(ctx(t), "gpu1 -$ anime: characters * poses")
	check(t, err)
	if !hasWarning(pv, "TooManyJobs") || !strings.Contains(warning(pv, "TooManyJobs").Message, "9 jobs") {
		t.Errorf("warnings = %+v", pv.Warnings)
	}
	pv, err = e.Commands.Preview(ctx(t), "gpu1 -$ anime: characters")
	check(t, err)
	if hasWarning(pv, "TooManyJobs") {
		t.Errorf("warnings = %+v", pv.Warnings)
	}
}

func hasWarning(pv *command.Preview, code string) bool { return warning(pv, code).Code != "" }

func warning(pv *command.Preview, code string) command.Warning {
	for _, w := range pv.Warnings {
		if w.Code == code {
			return w
		}
	}
	return command.Warning{}
}

func TestPreviewWarnsAboutServers(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	_, err := e.Items.Create(ctx(t), w.characters.ID, domain.Item{CodeName: "styled", LoRAs: []domain.LoRA{{Name: "style/one", StrengthModel: 1, StrengthClip: 1}, {Name: "two.safetensors"}}})
	check(t, err)
	code := "gpu1, gpu2 -$ anime: characters(styled) > upscale on gpu2"

	// Both offline.
	pv, err := e.Commands.Preview(ctx(t), code)
	check(t, err)
	var offline []string
	for _, w := range pv.Warnings {
		if w.Code == "ServerOffline" {
			offline = append(offline, code[w.Position.Offset:w.Position.End()])
		}
	}
	if !reflect.DeepEqual(offline, []string{"gpu1", "gpu2"}) {
		t.Errorf("offline warnings on %v (warnings %+v)", offline, pv.Warnings)
	}
	if hasWarning(pv, "LoRAMissing") {
		t.Error("the LoRAs of an offline server cannot be checked")
	}

	// Online, without the LoRAs.
	e.fleet.online[w.gpu1.ID], e.fleet.online[w.gpu2.ID] = true, true
	e.fleet.clients[w.gpu1.Host] = &fakeClient{loras: []string{"style/one.safetensors", "two.safetensors"}}
	e.fleet.clients[w.gpu2.Host] = &fakeClient{loras: []string{"two.safetensors"}}
	pv, err = e.Commands.Preview(ctx(t), code)
	check(t, err)
	if hasWarning(pv, "ServerOffline") {
		t.Errorf("warnings = %+v", pv.Warnings)
	}
	var missing []string
	for _, w := range pv.Warnings {
		if w.Code == "LoRAMissing" {
			missing = append(missing, w.Message)
		}
	}
	if len(missing) != 1 || !strings.Contains(missing[0], `"gpu2"`) || !strings.Contains(missing[0], `"style/one"`) {
		t.Errorf("missing = %v: only gpu2 lacks style/one", missing)
	}

	// A server that cannot list its models is reported, not fatal.
	e.fleet.clients[w.gpu2.Host].err = errors.New("ServerUnreachable", "no answer")
	pv, err = e.Commands.Preview(ctx(t), code)
	check(t, err)
	if !hasWarning(pv, "ModelsUnknown") || !pv.Valid {
		t.Errorf("warnings = %+v", pv.Warnings)
	}
	e.fleet.err = errors.New("ServerUnreachable", "no connection")
	pv, err = e.Commands.Preview(ctx(t), code)
	check(t, err)
	if !hasWarning(pv, "ModelsUnknown") {
		t.Errorf("warnings = %+v", pv.Warnings)
	}
}

func TestPreviewDoesNotLookForLoRAsInHugeCommands(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	e.fleet.online[w.gpu1.ID] = true
	e.fleet.clients[w.gpu1.Host] = &fakeClient{}
	lines := func(prefix string, n int) string {
		var b strings.Builder
		for i := range n {
			fmt.Fprintf(&b, "%s%d\n", prefix, i)
		}
		return b.String()
	}
	for _, g := range []struct {
		code string
		n    int
	}{{"big1", 101}, {"big2", 100}} {
		_, err := e.Groups.ImportText(ctx(t), domain.Group{Name: g.code, CodeName: g.code}, lines(g.code+"_", g.n))
		check(t, err)
	}
	big1, err := e.repos.Groups().GetByCode(ctx(t), "big1")
	check(t, err)
	_, err = e.Items.Create(ctx(t), big1.ID, domain.Item{CodeName: "styled", LoRAs: []domain.LoRA{{Name: "missing"}}})
	check(t, err)

	small, err := e.Commands.Preview(ctx(t), "gpu1 -$ anime: big1(styled) * big2 limit 5")
	check(t, err)
	if !hasWarning(small, "LoRAMissing") {
		t.Errorf("warnings = %+v", small.Warnings)
	}
	huge, err := e.Commands.Preview(ctx(t), "gpu1 -$ anime: big1 * big2")
	check(t, err)
	if huge.Jobs != 102*100 || hasWarning(huge, "LoRAMissing") {
		t.Errorf("jobs = %d, warnings = %+v", huge.Jobs, huge.Warnings)
	}
}

func TestPreviewWithoutFleetGivesNoServerWarnings(t *testing.T) {
	e := newEnv(t, func(d *Deps) { d.Fleet = nil })
	e.world(t)
	pv, err := e.Commands.Preview(ctx(t), "gpu1 -$ anime: characters")
	check(t, err)
	if !pv.Valid || len(pv.Warnings) != 0 {
		t.Errorf("preview = %+v", pv)
	}
}

// failingGroups fails every read of a group by code name.
type failingGroups struct {
	GroupStore
	err error
}

func (f failingGroups) GetByCode(context.Context, string) (*domain.Group, error) { return nil, f.err }

type failingRepos struct {
	Repos
	err error
}

func (f failingRepos) Groups() GroupStore {
	return failingGroups{GroupStore: f.Repos.Groups(), err: f.err}
}

type failingStore struct {
	Store
	err error
}

func (f failingStore) Repos() Repos { return failingRepos{Repos: f.Store.Repos(), err: f.err} }

// A store failure while the command is being checked is an error, never a
// "group not found" problem in the command.
func TestStoreFailureWhileReadingTheCatalogue(t *testing.T) {
	boom := errors.New("StoreFailure", "the database is gone", "operation", "read group")
	e := newEnv(t, withStore(func(s Store) Store { return failingStore{Store: s, err: boom} }))
	w := e.world(t)
	for name, call := range map[string]func() error{
		"Preview": func() error { _, err := e.Commands.Preview(ctx(t), "gpu1 -$ anime: characters"); return err },
		"Add": func() error {
			_, err := e.Commands.Add(ctx(t), w.project.ID, CommandInput{Code: "gpu1 -$ anime: characters"})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := call()
			appErr, ok := errors.FromError(err)
			if !ok || appErr != boom {
				t.Errorf("error = %v, want the store failure itself", err)
			}
		})
	}
}
