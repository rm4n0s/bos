package service

import (
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

func TestServers(t *testing.T) {
	e := newEnv(t)
	v, err := e.Servers.Create(ctx(t), domain.Server{Name: "Main", Host: "http://gpu1:8188", CodeName: "gpu1"})
	check(t, err)
	if v.ID == 0 || v.MaxConcurrentJobs != 1 || v.Online {
		t.Errorf("created = %+v, want an id, the default of one job and offline", v)
	}

	e.fleet.online[v.ID] = true
	got, err := e.Servers.Get(ctx(t), v.ID)
	check(t, err)
	if !got.Online {
		t.Error("a server the fleet reports available is not online")
	}

	upd, err := e.Servers.Update(ctx(t), v.ID, domain.Server{Name: "Renamed", Host: "https://gpu1.example:443", CodeName: "gpu1", IsLocal: true, MaxConcurrentJobs: 3})
	check(t, err)
	if upd.Name != "Renamed" || !upd.IsLocal || upd.MaxConcurrentJobs != 3 || !upd.CreatedAt.Equal(v.CreatedAt) || !upd.UpdatedAt.After(v.UpdatedAt) || !upd.Online {
		t.Errorf("updated = %+v (created %+v)", upd, v)
	}

	e.server(t, "gpu2")
	page, err := e.Servers.List(ctx(t), domain.Page{Size: 1})
	check(t, err)
	if page.Total != 2 || len(page.Items) != 1 || page.TotalPages != 2 || !page.Items[0].Online {
		t.Errorf("page = %+v", page)
	}

	check(t, e.Servers.Delete(ctx(t), v.ID))
	_, err = e.Servers.Get(ctx(t), v.ID)
	wantRoute(t, err, "ServerService.Get", "ServerNotFound")
}

func TestServerInputIsChecked(t *testing.T) {
	e := newEnv(t)
	good := domain.Server{Name: "n", Host: "http://h:1", CodeName: "ok", MaxConcurrentJobs: 1}
	tests := []struct {
		name   string
		change func(*domain.Server)
		tag    string
	}{
		{"blank name", func(s *domain.Server) { s.Name = "  " }, "NameInvalid"},
		{"long name", func(s *domain.Server) { s.Name = strings.Repeat("n", 101) }, "NameInvalid"},
		{"no scheme", func(s *domain.Server) { s.Host = "gpu1:8188" }, "ServerHostInvalid"},
		{"ftp", func(s *domain.Server) { s.Host = "ftp://gpu1" }, "ServerHostInvalid"},
		{"no host", func(s *domain.Server) { s.Host = "http://" }, "ServerHostInvalid"},
		{"long host", func(s *domain.Server) { s.Host = "http://" + strings.Repeat("h", 100) }, "ServerHostInvalid"},
		{"space in code", func(s *domain.Server) { s.CodeName = "gpu 1" }, "CodeNameInvalid"},
		{"empty code", func(s *domain.Server) { s.CodeName = "" }, "CodeNameInvalid"},
		{"long code", func(s *domain.Server) { s.CodeName = strings.Repeat("c", 101) }, "CodeNameInvalid"},
		{"reserved code", func(s *domain.Server) { s.CodeName = "where" }, "CodeNameReserved"},
		{"negative jobs", func(s *domain.Server) { s.MaxConcurrentJobs = -1 }, "ServerMaxJobsInvalid"},
		{"long folder", func(s *domain.Server) { s.ComfyUIPath = "/" + strings.Repeat("d", 500) }, "ServerComfyPathInvalid"},
		{"line break in the folder", func(s *domain.Server) { s.ComfyUIPath = "/home/me\n/ComfyUI" }, "ServerComfyPathInvalid"},
		{"NUL in the folder", func(s *domain.Server) { s.ComfyUIPath = "/home/me\x00" }, "ServerComfyPathInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := good
			tt.change(&in)
			_, err := e.Servers.Create(ctx(t), in)
			wantRoute(t, err, "ServerService.Create", tt.tag)
			s := e.server(t, "target"+strings.ReplaceAll(tt.name, " ", ""))
			_, err = e.Servers.Update(ctx(t), s.ID, in)
			wantRoute(t, err, "ServerService.Update", tt.tag)
		})
	}
	if page, _ := e.Servers.List(ctx(t), domain.Page{}); page.Total != len(tests) {
		t.Errorf("%d servers, want only the %d targets of the updates: a rejected create stored something", page.Total, len(tests))
	}
}

func TestServerFailures(t *testing.T) {
	e := newEnv(t)
	e.server(t, "gpu1")
	_, err := e.Servers.Create(ctx(t), domain.Server{Name: "n", Host: "http://h:1", CodeName: "gpu1"})
	wantRoute(t, err, "ServerService.Create", "ServerCodeNameTaken")
	_, err = e.Servers.Update(ctx(t), 999, domain.Server{Name: "n", Host: "http://h:1", CodeName: "x"})
	wantRoute(t, err, "ServerService.Update", "ServerNotFound")
	wantRoute(t, e.Servers.Delete(ctx(t), 999), "ServerService.Delete", "ServerNotFound")
	_, err = e.Servers.Get(ctx(t), 999)
	wantRoute(t, err, "ServerService.Get", "ServerNotFound")
}

func TestServerLoRAs(t *testing.T) {
	e := newEnv(t)
	s := e.server(t, "gpu1")
	e.fleet.clients[s.Host] = &fakeClient{loras: []string{"a.safetensors", "style/b.safetensors"}}
	got, err := e.Servers.LoRAs(ctx(t), s.ID)
	check(t, err)
	if !reflect.DeepEqual(got, []string{"a.safetensors", "style/b.safetensors"}) {
		t.Errorf("LoRAs = %v", got)
	}

	_, err = e.Servers.LoRAs(ctx(t), 999)
	wantRoute(t, err, "ServerService.LoRAs", "ServerNotFound")

	// A failure of the fleet or of the server comes back untouched, with the
	// tag it was given where it happened.
	unreachable := errors.New("ServerUnreachable", "no answer")
	e.fleet.err = unreachable
	_, err = e.Servers.LoRAs(ctx(t), s.ID)
	if appErr, ok := errors.FromError(err); !ok || appErr != unreachable {
		t.Errorf("fleet failure came back as %v", err)
	}

	e.fleet.err = nil
	e.fleet.clients[s.Host].err = unreachable
	_, err = e.Servers.LoRAs(ctx(t), s.ID)
	if appErr, ok := errors.FromError(err); !ok || appErr != unreachable {
		t.Errorf("model list failure came back as %v", err)
	}
}

func TestServerWithoutFleet(t *testing.T) {
	e := newEnv(t, func(d *Deps) { d.Fleet = nil })
	s := e.server(t, "gpu1")
	if got, _ := e.Servers.Get(ctx(t), s.ID); got.Online {
		t.Error("a server is online without a fleet to say so")
	}
	_, err := e.Servers.LoRAs(ctx(t), s.ID)
	wantRoute(t, err, "ServerService.LoRAs", "ServerUnreachable")
}

func TestServerTest(t *testing.T) {
	e := newEnv(t)
	s := e.server(t, "gpu1")
	check(t, e.Servers.Test(ctx(t), s.ID))

	wantRoute(t, e.Servers.Test(ctx(t), 999), "ServerService.Test", "ServerNotFound")

	// What the connection says is passed on untouched.
	down := errors.New("ServerUnreachable", "no route to host")
	e.fleet.err = down
	if err := e.Servers.Test(ctx(t), s.ID); err != error(down) { //nolint:errorlint // the same error value must come back
		t.Errorf("err = %v, want the fleet's own error", err)
	}
}

func TestServerComfyUIPath(t *testing.T) {
	e := newEnv(t)
	// Optional, and kept without the spaces around it.
	v, err := e.Servers.Create(ctx(t), domain.Server{Name: "Main", Host: "http://gpu1:8188", CodeName: "gpu1", ComfyUIPath: "  /home/me/ComfyUI \n"})
	check(t, err)
	if v.ComfyUIPath != "/home/me/ComfyUI" {
		t.Errorf("created with %q", v.ComfyUIPath)
	}
	got, err := e.Servers.Get(ctx(t), v.ID)
	check(t, err)
	if got.ComfyUIPath != "/home/me/ComfyUI" {
		t.Errorf("stored %q", got.ComfyUIPath)
	}
	// An update replaces it, and an empty one clears it.
	upd, err := e.Servers.Update(ctx(t), v.ID, domain.Server{Name: "Main", Host: "http://gpu1:8188", CodeName: "gpu1", ComfyUIPath: "D:\\ComfyUI"})
	check(t, err)
	if upd.ComfyUIPath != "D:\\ComfyUI" {
		t.Errorf("updated with %q", upd.ComfyUIPath)
	}
	upd, err = e.Servers.Update(ctx(t), v.ID, domain.Server{Name: "Main", Host: "http://gpu1:8188", CodeName: "gpu1"})
	check(t, err)
	if upd.ComfyUIPath != "" {
		t.Errorf("cleared with %q", upd.ComfyUIPath)
	}
}

// The servers the LoRA imports offer are those that have a folder, by name.
func TestServersWithAComfyUIPath(t *testing.T) {
	e := newEnv(t)
	mk := func(code, name, path string) {
		_, err := e.Servers.Create(ctx(t), domain.Server{Name: name, Host: "http://" + code + ":8188", CodeName: code, ComfyUIPath: path})
		check(t, err)
	}
	mk("c", "Zeta", "/z")
	mk("b", "No folder", "")
	mk("a", "alpha", "/a")
	mk("d", "Beta", "/b")
	got, err := e.Servers.WithComfyUIPath(ctx(t))
	check(t, err)
	var names []string
	for _, s := range got {
		names = append(names, s.Name+"="+s.ComfyUIPath)
	}
	if want := []string{"alpha=/a", "Beta=/b", "Zeta=/z"}; !reflect.DeepEqual(names, want) {
		t.Errorf("servers = %v, want %v", names, want)
	}
}
