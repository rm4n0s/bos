package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
	"modernc.org/sqlite"
)

// SQLite result codes for constraint failures.
const (
	sqlitePrimaryKey = 1555 // SQLITE_CONSTRAINT_PRIMARYKEY
	sqliteUnique     = 2067 // SQLITE_CONSTRAINT_UNIQUE
	sqliteForeignKey = 787  // SQLITE_CONSTRAINT_FOREIGNKEY
)

// dbtx is what both *sql.DB and *sql.Tx offer, so a repository works on the
// pool or inside a transaction.
type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Repos is the set of repositories over one connection source: the pool, or a
// single transaction.
type Repos struct {
	Servers    *ServerRepo
	Generators *GeneratorRepo
	Fixers     *FixerRepo
	Groups     *GroupRepo
	Items      *ItemRepo
	Tags       *TagRepo
	Projects   *ProjectRepo
	Commands   *CommandRepo
	Jobs       *JobRepo
}

// Repos returns the repositories on the connection pool. A method that needs
// several statements to be atomic opens its own transaction.
func (d *DB) Repos() *Repos { return newRepos(d.sql, d.clock) }

// InTx runs fn in one transaction on the repositories it is given and commits
// when fn returns nil. It rolls back on an error or a panic. fn must use only
// those repositories: the transaction holds the write lock, and a call through
// the pool would wait for it.
func (d *DB) InTx(ctx context.Context, fn func(r *Repos) error) (err error) {
	tx, beginErr := d.sql.BeginTx(ctx, nil)
	if beginErr != nil {
		return errors.NewErr("StoreFailure", beginErr).SetMetadata("operation", "begin")
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback() // the panic is re-raised; nothing can be done with a second error
			panic(p)
		}
	}()
	if err := fn(newRepos(tx, d.clock)); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			if appErr, ok := errors.FromError(err); ok {
				appErr.Metadata = withKey(appErr.Metadata, "rollback_error", rbErr.Error())
			}
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return errors.NewErr("StoreFailure", err).SetMetadata("operation", "commit")
	}
	return nil
}

func withKey(m map[string]any, k string, v any) map[string]any {
	if m == nil {
		m = map[string]any{}
	}
	m[k] = v
	return m
}

func newRepos(q dbtx, clock func() time.Time) *Repos {
	b := base{q: q, clock: clock}
	items := &ItemRepo{base: b}
	jobs := &JobRepo{base: b}
	commands := &CommandRepo{base: b, jobs: jobs}
	return &Repos{
		Servers:    &ServerRepo{base: b},
		Generators: &GeneratorRepo{base: b},
		Fixers:     &FixerRepo{base: b},
		Groups:     &GroupRepo{base: b, items: items},
		Items:      items,
		Tags:       &TagRepo{base: b},
		Projects:   &ProjectRepo{base: b, commands: commands, jobs: jobs},
		Commands:   commands,
		Jobs:       jobs,
	}
}

// base is what every repository shares.
type base struct {
	q     dbtx
	clock func() time.Time
}

func (b base) now() time.Time { return b.clock().UTC() }

// atomically runs fn in a transaction: the one the repository is already in, or
// a new one when it works on the pool.
func (b base) atomically(ctx context.Context, fn func(q dbtx) error) error {
	db, ok := b.q.(*sql.DB)
	if !ok {
		return fn(b.q)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return errors.NewErr("StoreFailure", err).SetMetadata("operation", "begin")
	}
	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			if appErr, ok := errors.FromError(err); ok {
				appErr.Metadata = withKey(appErr.Metadata, "rollback_error", rbErr.Error())
			}
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return errors.NewErr("StoreFailure", err).SetMetadata("operation", "commit")
	}
	return nil
}

// ---- errors ----

func storeFailure(err error, operation string) *errors.Error {
	return errors.NewErr("StoreFailure", err).SetMetadata("operation", operation)
}

func sqliteCode(err error) int {
	var se *sqlite.Error
	if errors.As(err, &se) {
		return se.Code()
	}
	return 0
}

func isUnique(err error) bool {
	c := sqliteCode(err)
	return c == sqliteUnique || c == sqlitePrimaryKey
}

func isForeignKey(err error) bool { return sqliteCode(err) == sqliteForeignKey }

func notFound(tag, what string, id int64) *errors.Error {
	return errors.New(tag, what+" not found", "id", id)
}

// closeRows closes rows after a failure and keeps the first error.
func closeRows(rows *sql.Rows, err error) error {
	if closeErr := rows.Close(); closeErr != nil {
		if appErr, ok := errors.FromError(err); ok {
			appErr.Metadata = withKey(appErr.Metadata, "close_error", closeErr.Error())
		}
	}
	return err
}

// ---- values ----

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, errors.NewErr("StoreFailure", err).SetMetadata("operation", "parse time", "value", s)
	}
	return t, nil
}

// encode writes v as JSON text. A nil slice or map becomes [] or {} through the
// fallback, so the column never holds null.
func encode(v any, fallback string) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", storeFailure(err, "encode")
	}
	if s := string(b); s != "null" {
		return s, nil
	}
	return fallback, nil
}

func decode(text string, v any) error {
	if err := json.Unmarshal([]byte(text), v); err != nil {
		return storeFailure(err, "decode")
	}
	return nil
}

// scanner is *sql.Row or *sql.Rows.
type scanner interface{ Scan(dest ...any) error }

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// placeholders returns "?, ?, …" for n values.
func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

// nullInt converts an optional id for a nullable column.
func nullInt(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

// pageArgs returns LIMIT and OFFSET for a page.
func pageArgs(p domain.Page) (limit, offset int) {
	n := p.Normalised()
	return n.Size, n.Offset()
}
