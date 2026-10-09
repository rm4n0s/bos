package store

import (
	"context"
	"embed"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// migration is one numbered SQL file (SPEC §14.4, D8).
type migration struct {
	version int
	name    string
	sql     string
}

func loadMigrations(fsys fs.FS, dir string) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, errors.NewErr("MigrationFailed", err).SetMetadata("dir", dir)
	}
	var out []migration
	seen := map[int]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(name, "_")
		version, convErr := strconv.Atoi(prefix)
		if !ok || convErr != nil || version < 1 {
			return nil, errors.New("MigrationFailed", "migration file name must start with a positive number and an underscore", "file", name)
		}
		if other, dup := seen[version]; dup {
			return nil, errors.New("MigrationFailed", "two migrations have the same version", "version", version, "files", other+", "+name)
		}
		seen[version] = name
		body, err := fs.ReadFile(fsys, dir+"/"+name)
		if err != nil {
			return nil, errors.NewErr("MigrationFailed", err).SetMetadata("file", name)
		}
		out = append(out, migration{version: version, name: name, sql: string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// Migrate creates the schema, or brings it up to date, by applying the embedded
// migrations that the version table does not list yet, in order. Each one runs
// in its own transaction, so a failure leaves the database at the last version
// that worked.
func (d *DB) Migrate(ctx context.Context) error {
	ms, err := loadMigrations(migrationFiles, "migrations")
	if err != nil {
		return err
	}
	return d.applyMigrations(ctx, ms)
}

func (d *DB) applyMigrations(ctx context.Context, ms []migration) error {
	if _, err := d.sql.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT NOT NULL,
		applied_at TEXT NOT NULL)`); err != nil {
		return errors.NewErr("MigrationFailed", err).SetMetadata("step", "version table")
	}
	applied, err := d.appliedVersions(ctx)
	if err != nil {
		return err
	}
	for _, m := range ms {
		if applied[m.version] {
			continue
		}
		if err := d.applyOne(ctx, m); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) appliedVersions(ctx context.Context) (map[int]bool, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, errors.NewErr("MigrationFailed", err).SetMetadata("step", "read versions")
	}
	applied := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, closeRows(rows, errors.NewErr("MigrationFailed", err).SetMetadata("step", "read versions"))
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		return nil, closeRows(rows, errors.NewErr("MigrationFailed", err).SetMetadata("step", "read versions"))
	}
	if err := rows.Close(); err != nil {
		return nil, errors.NewErr("MigrationFailed", err).SetMetadata("step", "read versions")
	}
	return applied, nil
}

func (d *DB) applyOne(ctx context.Context, m migration) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return errors.NewErr("MigrationFailed", err).SetMetadata("version", m.version, "file", m.name)
	}
	fail := func(err error) error {
		wrapped := errors.NewErr("MigrationFailed", err).SetMetadata("version", m.version, "file", m.name)
		if rbErr := tx.Rollback(); rbErr != nil {
			return wrapped.SetMetadata("version", m.version, "file", m.name, "rollback_error", rbErr.Error())
		}
		return wrapped
	}
	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return fail(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
		m.version, m.name, formatTime(d.clock())); err != nil {
		return fail(err)
	}
	if err := tx.Commit(); err != nil {
		return errors.NewErr("MigrationFailed", err).SetMetadata("version", m.version, "file", m.name)
	}
	return nil
}

// SchemaVersion returns the highest migration applied, or 0 for an empty
// database.
func (d *DB) SchemaVersion(ctx context.Context) (int, error) {
	var v int
	err := d.sql.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&v)
	if err != nil {
		return 0, errors.NewErr("StoreFailure", err).SetMetadata("operation", "schema version")
	}
	return v, nil
}
