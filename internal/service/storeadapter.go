package service

import (
	"context"

	"github.com/rm4n0s/bos/internal/store"
)

// NewStore adapts the SQLite store to the Store port.
func NewStore(db *store.DB) Store { return storeAdapter{db: db} }

type storeAdapter struct{ db *store.DB }

func (s storeAdapter) Repos() Repos { return repos{r: s.db.Repos()} }

func (s storeAdapter) InTx(ctx context.Context, fn func(Repos) error) error {
	return s.db.InTx(ctx, func(r *store.Repos) error { return fn(repos{r: r}) })
}

type repos struct{ r *store.Repos }

func (r repos) Servers() ServerStore       { return r.r.Servers }
func (r repos) Generators() GeneratorStore { return r.r.Generators }
func (r repos) Fixers() FixerStore         { return r.r.Fixers }
func (r repos) Groups() GroupStore         { return r.r.Groups }
func (r repos) Items() ItemStore           { return r.r.Items }
func (r repos) Tags() TagStore             { return r.r.Tags }
func (r repos) Projects() ProjectStore     { return r.r.Projects }
func (r repos) Commands() CommandStore     { return r.r.Commands }
func (r repos) Jobs() JobStore             { return r.r.Jobs }
func (r repos) Settings() SettingsStore    { return r.r.Settings }
