// Package service holds the use cases of the application (SPEC §14.2): what
// the REST API and the web interface both call. It contains the business
// rules; the front ends only translate requests and answers.
//
// The service reaches the database, the file system and the ComfyUI servers
// through the small interfaces below, so tests replace them.
package service

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"io"
	"log/slog"

	"github.com/rm4n0s/bos/internal/comfy"
	"github.com/rm4n0s/bos/internal/command"
	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

// ---- ports ----

// Store is the persistent data (SPEC §14.3, rule 2). InTx runs fn in one
// transaction on the repositories it is given.
type Store interface {
	Repos() Repos
	InTx(ctx context.Context, fn func(Repos) error) error
}

// Repos is the set of repositories of one connection source.
type Repos interface {
	Servers() ServerStore
	Generators() GeneratorStore
	Fixers() FixerStore
	Groups() GroupStore
	Items() ItemStore
	Tags() TagStore
	Projects() ProjectStore
	Commands() CommandStore
	Jobs() JobStore
}

// ServerStore keeps servers.
type ServerStore interface {
	Create(ctx context.Context, v *domain.Server) error
	Update(ctx context.Context, v *domain.Server) error
	Delete(ctx context.Context, id domain.ID) error
	Get(ctx context.Context, id domain.ID) (*domain.Server, error)
	GetByCode(ctx context.Context, code string) (*domain.Server, error)
	All(ctx context.Context) ([]domain.Server, error)
	List(ctx context.Context, p domain.Page) (domain.Paged[domain.Server], error)
}

// GeneratorStore keeps generators.
type GeneratorStore interface {
	Create(ctx context.Context, v *domain.Generator) error
	Update(ctx context.Context, v *domain.Generator) error
	Delete(ctx context.Context, id domain.ID) error
	Get(ctx context.Context, id domain.ID) (*domain.Generator, error)
	GetByCode(ctx context.Context, code string) (*domain.Generator, error)
	List(ctx context.Context, p domain.Page) (domain.Paged[domain.Generator], error)
}

// FixerStore keeps fixers.
type FixerStore interface {
	Create(ctx context.Context, v *domain.Fixer) error
	Update(ctx context.Context, v *domain.Fixer) error
	Delete(ctx context.Context, id domain.ID) error
	Get(ctx context.Context, id domain.ID) (*domain.Fixer, error)
	GetByCode(ctx context.Context, code string) (*domain.Fixer, error)
	List(ctx context.Context, p domain.Page) (domain.Paged[domain.Fixer], error)
}

// GroupStore keeps groups with their group tags. Delete returns the files to
// remove once the deletion has committed.
type GroupStore interface {
	Create(ctx context.Context, v *domain.Group) error
	Update(ctx context.Context, v *domain.Group) error
	Delete(ctx context.Context, id domain.ID) ([]string, error)
	Get(ctx context.Context, id domain.ID) (*domain.Group, error)
	GetByCode(ctx context.Context, code string) (*domain.Group, error)
	List(ctx context.Context, tag string, p domain.Page) (domain.Paged[domain.Group], error)
}

// ItemStore keeps items with their item tags.
type ItemStore interface {
	Create(ctx context.Context, v *domain.Item) error
	Update(ctx context.Context, v *domain.Item) error
	Delete(ctx context.Context, id domain.ID) ([]string, error)
	Get(ctx context.Context, id domain.ID) (*domain.Item, error)
	ListByGroup(ctx context.Context, group domain.ID, p domain.Page) (domain.Paged[domain.Item], error)
	AllByGroup(ctx context.Context, group domain.ID) ([]domain.Item, error)
}

// TagStore lists and renames tags.
type TagStore interface {
	List(ctx context.Context, scope domain.TagScope, prefix string) ([]domain.TagCount, error)
	Rename(ctx context.Context, scope domain.TagScope, from, to string) (int, error)
}

// ProjectStore keeps projects. Delete removes the project with its commands
// and jobs and returns the files to remove.
type ProjectStore interface {
	Create(ctx context.Context, v *domain.Project) error
	Update(ctx context.Context, v *domain.Project) error
	Delete(ctx context.Context, id domain.ID) ([]string, error)
	Get(ctx context.Context, id domain.ID) (*domain.Project, error)
	List(ctx context.Context, p domain.Page) (domain.Paged[domain.Project], error)
}

// CommandStore keeps commands and their order within a project.
type CommandStore interface {
	Create(ctx context.Context, v *domain.Command, insertAt int) error
	Update(ctx context.Context, v *domain.Command) error
	SetStatus(ctx context.Context, id domain.ID, status domain.Status) error
	Get(ctx context.Context, id domain.ID) (*domain.Command, error)
	ListByProject(ctx context.Context, project domain.ID, p domain.Page) (domain.Paged[domain.CommandSummary], error)
	AllByProject(ctx context.Context, project domain.ID) ([]domain.Command, error)
	FindByText(ctx context.Context, needle string) ([]domain.Command, error)
	Delete(ctx context.Context, id domain.ID) ([]string, error)
	Move(ctx context.Context, id domain.ID, order int) (int, error)
	Renumber(ctx context.Context, project domain.ID) error
}

// JobStore keeps jobs.
type JobStore interface {
	CreateMany(ctx context.Context, jobs []*domain.Job) error
	DeleteByCommand(ctx context.Context, command domain.ID) ([]string, error)
}

// Files is the file storage the services use: they remove the files of what
// they delete, and read the reference image of a region mask.
type Files interface {
	Remove(path string) error
	Open(path string) (io.ReadCloser, error)
}

// Fleet is what the services know about the ComfyUI servers: whether each
// answers, and a connection to ask what it has installed. The scheduler
// provides it (SPEC §8.1).
type Fleet interface {
	// Available reports whether the last probe of the server succeeded.
	Available(serverID domain.ID) bool
	// Client returns the connection to a server.
	Client(ctx context.Context, s domain.Server) (comfy.Client, error)
}

// ---- construction ----

// Dirs are the storage locations the services write names under.
type Dirs struct {
	// Results is where the result file of every job is planned (SPEC §7.1).
	Results string
	// Regions is where the generated masks of a region reference image go.
	Regions string
}

// Limits are the configured bounds of a command (SPEC §2, §6.11).
type Limits struct {
	// PreviewSoftLimit is the job count above which a preview warns; 0 means none.
	PreviewSoftLimit int
	// MaxCombinations and MaxJobs are the hard limits of the expander; 0 uses its defaults.
	MaxCombinations int
	MaxJobs         int
}

func (l Limits) options() command.Options {
	return command.Options{MaxCombinations: l.MaxCombinations, MaxJobs: l.MaxJobs}
}

// Deps is everything the services need.
type Deps struct {
	Store  Store
	Files  Files
	Fleet  Fleet // optional: without it the preview gives no server warnings
	Dirs   Dirs
	Limits Limits
	Logger *slog.Logger
	// NewSeed returns a random 64-bit number; the default reads crypto/rand.
	NewSeed func() uint64
}

// Services are all the use cases, built once in main and shared.
type Services struct {
	Servers    *ServerService
	Generators *GeneratorService
	Fixers     *FixerService
	Groups     *GroupService
	Items      *ItemService
	Tags       *TagService
	Projects   *ProjectService
	Commands   *CommandService
}

// New builds the services over deps.
func New(deps Deps) *Services {
	if deps.Logger == nil {
		deps.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if deps.NewSeed == nil {
		deps.NewSeed = randomSeed
	}
	b := base{Deps: deps}
	return &Services{
		Servers:    &ServerService{base: b},
		Generators: &GeneratorService{base: b},
		Fixers:     &FixerService{base: b},
		Groups:     &GroupService{base: b},
		Items:      &ItemService{base: b},
		Tags:       &TagService{base: b},
		Projects:   &ProjectService{base: b},
		Commands:   &CommandService{base: b},
	}
}

func randomSeed() uint64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand does not fail on supported systems
	}
	return binary.BigEndian.Uint64(b[:])
}

// base is what every service shares.
type base struct{ Deps }

// removeFiles deletes the files a committed deletion left behind. A file that
// cannot be removed is logged, not returned: the data is gone and the caller
// cannot undo it (SPEC §17.4).
func (b base) removeFiles(paths []string) {
	for _, p := range paths {
		if err := b.Files.Remove(p); err != nil {
			logError(b.Logger, "could not remove a file", err, "path", p)
		}
	}
}

// logError logs err once, with the replayable form of an application error
// (SPEC §17.4).
func logError(l *slog.Logger, msg string, err error, attrs ...any) {
	if appErr, ok := errors.FromError(err); ok {
		if j, jerr := appErr.ToJson(); jerr == nil {
			attrs = append(attrs, "error", j)
			l.Warn(msg, attrs...)
			return
		}
	}
	l.Warn(msg, append(attrs, "error", err.Error())...)
}
