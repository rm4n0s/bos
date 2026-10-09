package store

import (
	"testing"
	"testing/fstest"
)

func TestMigrateCreatesSchemaOnce(t *testing.T) {
	db := newDB(t) // Migrate already ran once
	v, err := db.SchemaVersion(ctx(t))
	check(t, err)
	if v != 1 {
		t.Errorf("schema version = %d, want 1", v)
	}
	check(t, db.Migrate(ctx(t))) // idempotent
	var n int
	check(t, db.sql.QueryRowContext(ctx(t), `SELECT COUNT(*) FROM schema_migrations`).Scan(&n))
	if n != 1 {
		t.Errorf("%d versions recorded, want 1", n)
	}
	for _, table := range []string{"servers", "generators", "fixers", "groups", "group_tags", "items", "item_tags", "projects", "commands", "jobs"} {
		if err := db.sql.QueryRowContext(ctx(t), `SELECT COUNT(*) FROM `+table).Scan(&n); err != nil {
			t.Errorf("table %s: %v", table, err)
		}
	}
}

func TestMigrationsApplyInOrderAndKeepGoing(t *testing.T) {
	db, err := Open(ctx(t), InMemory, WithClock(fakeClock()))
	check(t, err)
	t.Cleanup(func() { check(t, db.Close()) })

	fsys := fstest.MapFS{
		"m/0002_second.sql": {Data: []byte(`ALTER TABLE t ADD COLUMN b TEXT;`)},
		"m/0001_first.sql":  {Data: []byte(`CREATE TABLE t (a INTEGER);`)},
		"m/readme.txt":      {Data: []byte(`ignored`)},
	}
	ms, err := loadMigrations(fsys, "m")
	check(t, err)
	if len(ms) != 2 || ms[0].version != 1 || ms[1].version != 2 {
		t.Fatalf("migrations = %+v", ms)
	}
	check(t, db.applyMigrations(ctx(t), ms[:1]))
	if v, _ := db.SchemaVersion(ctx(t)); v != 1 {
		t.Errorf("version = %d", v)
	}
	// A later run applies only what is new.
	check(t, db.applyMigrations(ctx(t), ms))
	if v, _ := db.SchemaVersion(ctx(t)); v != 2 {
		t.Errorf("version = %d", v)
	}
	if _, err := db.sql.ExecContext(ctx(t), `INSERT INTO t (a, b) VALUES (1, 'x')`); err != nil {
		t.Errorf("second migration not applied: %v", err)
	}
}

func TestMigrationFailureRollsBack(t *testing.T) {
	db, err := Open(ctx(t), InMemory, WithClock(fakeClock()))
	check(t, err)
	t.Cleanup(func() { check(t, db.Close()) })
	ms := []migration{
		{version: 1, name: "0001_ok.sql", sql: `CREATE TABLE ok (a INTEGER);`},
		{version: 2, name: "0002_bad.sql", sql: `CREATE TABLE half (a INTEGER); THIS IS NOT SQL;`},
	}
	err = db.applyMigrations(ctx(t), ms)
	wantRoute(t, err, "applyMigrations", "MigrationFailed")
	if v, _ := db.SchemaVersion(ctx(t)); v != 1 {
		t.Errorf("version = %d, want 1 (the failed migration must not be recorded)", v)
	}
	var n int
	if err := db.sql.QueryRowContext(ctx(t), `SELECT COUNT(*) FROM half`).Scan(&n); err == nil {
		t.Error("the half-applied table survived the rollback")
	}
}

func TestLoadMigrationsRejectsBadFiles(t *testing.T) {
	tests := []struct {
		name string
		fsys fstest.MapFS
	}{
		{"no number", fstest.MapFS{"m/init.sql": {Data: []byte(`x`)}}},
		{"zero", fstest.MapFS{"m/0000_x.sql": {Data: []byte(`x`)}}},
		{"duplicate", fstest.MapFS{"m/1_a.sql": {Data: []byte(`x`)}, "m/0001_b.sql": {Data: []byte(`x`)}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadMigrations(tt.fsys, "m")
			wantRoute(t, err, "loadMigrations", "MigrationFailed")
		})
	}
	_, err := loadMigrations(fstest.MapFS{}, "missing")
	wantRoute(t, err, "loadMigrations", "MigrationFailed")
}

func TestMigrationNumbersOfEmbeddedFiles(t *testing.T) {
	ms, err := loadMigrations(migrationFiles, "migrations")
	check(t, err)
	for i, m := range ms {
		if m.version != i+1 {
			t.Errorf("migration %s has version %d at position %d: versions must be contiguous", m.name, m.version, i+1)
		}
	}
}
