package service

import (
	"context"
	"sort"
	"strings"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

// ServerService manages the ComfyUI servers (SPEC §3.1).
type ServerService struct{ base }

// ServerView is a server with its live status.
type ServerView struct {
	domain.Server
	// Online is true when the last probe of the server succeeded.
	Online bool `json:"online"`
}

func (s *ServerService) view(v domain.Server) ServerView {
	return ServerView{Server: v, Online: s.Fleet != nil && s.Fleet.Available(v.ID)}
}

func (s *ServerService) check(v *domain.Server) error {
	if err := checkName("server", v.Name, maxServerName); err != nil {
		return err
	}
	if err := checkHost(v.Host); err != nil {
		return err
	}
	if err := checkCodeName("server", v.CodeName); err != nil {
		return err
	}
	if v.MaxConcurrentJobs == 0 {
		v.MaxConcurrentJobs = 1
	}
	if v.MaxConcurrentJobs < 1 {
		return errors.New("ServerMaxJobsInvalid", "a server must run at least one job at a time", "max_concurrent_jobs", v.MaxConcurrentJobs)
	}
	return nil
}

// Create adds a server. A max_concurrent_jobs of 0 means the default, 1.
func (s *ServerService) Create(ctx context.Context, in domain.Server) (*ServerView, error) {
	if err := s.check(&in); err != nil {
		return nil, err
	}
	in.ID = 0
	if err := s.Store.Repos().Servers().Create(ctx, &in); err != nil {
		return nil, err
	}
	v := s.view(in)
	return &v, nil
}

// Update replaces the editable fields of a server.
func (s *ServerService) Update(ctx context.Context, id domain.ID, in domain.Server) (*ServerView, error) {
	if err := s.check(&in); err != nil {
		return nil, err
	}
	in.ID = id
	servers := s.Store.Repos().Servers()
	if err := servers.Update(ctx, &in); err != nil {
		return nil, err
	}
	// The update wrote the editable fields; read the row back for its dates.
	got, err := servers.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	v := s.view(*got)
	return &v, nil
}

// Delete removes a server. Jobs keep the host they ran on (SPEC §3.9).
func (s *ServerService) Delete(ctx context.Context, id domain.ID) error {
	return s.Store.Repos().Servers().Delete(ctx, id)
}

// Get returns a server with its status.
func (s *ServerService) Get(ctx context.Context, id domain.ID) (*ServerView, error) {
	got, err := s.Store.Repos().Servers().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	v := s.view(*got)
	return &v, nil
}

// List returns one page of servers with their status.
func (s *ServerService) List(ctx context.Context, p domain.Page) (domain.Paged[ServerView], error) {
	page, err := s.Store.Repos().Servers().List(ctx, p)
	if err != nil {
		return domain.Paged[ServerView]{}, err
	}
	views := make([]ServerView, len(page.Items))
	for i, v := range page.Items {
		views[i] = s.view(v)
	}
	return domain.Paged[ServerView]{Items: views, Total: page.Total, Page: page.Page, PageSize: page.PageSize, TotalPages: page.TotalPages}, nil
}

// LoRAs lists the LoRAs installed on a server (SPEC §9, last bullet).
func (s *ServerService) LoRAs(ctx context.Context, id domain.ID) ([]string, error) {
	srv, err := s.Store.Repos().Servers().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.Fleet == nil {
		return nil, errors.New("ServerUnreachable", "no connection to the servers is available", "server", srv.CodeName)
	}
	client, err := s.Fleet.Client(ctx, *srv)
	if err != nil {
		return nil, err
	}
	return client.ListModels(ctx, "loras")
}

// Test asks a server whether it answers (SPEC §16.3, "test connection"). An
// unanswered server is ServerUnreachable.
func (s *ServerService) Test(ctx context.Context, id domain.ID) error {
	srv, err := s.Store.Repos().Servers().Get(ctx, id)
	if err != nil {
		return err
	}
	if s.Fleet == nil {
		return errors.New("ServerUnreachable", "no connection to the servers is available", "server", srv.CodeName)
	}
	client, err := s.Fleet.Client(ctx, *srv)
	if err != nil {
		return err
	}
	return client.Ping(ctx)
}

// All returns every server, by name (not case sensitive): the choices of the
// LoRA imports.
func (s *ServerService) All(ctx context.Context) ([]domain.Server, error) {
	all, err := s.Store.Repos().Servers().All(ctx)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(all, func(i, j int) bool { return strings.ToLower(all[i].Name) < strings.ToLower(all[j].Name) })
	return all, nil
}
