package store

import (
	"testing"
	"time"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
	"gorm.io/gorm"
)

func TestMigrateCreatesSchemaOnce(t *testing.T) {
	db := newDB(t) // Migrate already ran once
	v, err := db.SchemaVersion(ctx(t))
	check(t, err)
	if v != len(migrations()) {
		t.Errorf("schema version = %d, want %d", v, len(migrations()))
	}
	check(t, db.Migrate(ctx(t))) // idempotent
	if n := rowCount(t, db, "schema_migrations"); n != len(migrations()) {
		t.Errorf("%d versions recorded, want %d", n, len(migrations()))
	}
	for _, table := range []string{"servers", "generators", "fixers", "groups", "group_tags", "items", "item_tags", "projects", "commands", "jobs"} {
		if !db.gorm.Migrator().HasTable(table) {
			t.Errorf("table %s is missing", table)
		}
	}
}

// The records the repositories use follow the latest schema. They must find
// every column they write in the tables the migrations build.
func TestRecordsMatchMigratedSchema(t *testing.T) {
	db := newDB(t)
	records := []any{
		&serverRecord{}, &generatorRecord{}, &fixerRecord{}, &groupRecord{}, &groupTagRecord{}, &itemRecord{},
		&itemTagRecord{}, &projectRecord{}, &commandRecord{}, &jobRecord{}, &settingRecord{},
	}
	m := db.gorm.Migrator()
	for _, rec := range records {
		stmt := &gorm.Statement{DB: db.gorm}
		check(t, stmt.Parse(rec))
		columns, err := m.ColumnTypes(rec)
		check(t, err)
		have := map[string]bool{}
		for _, c := range columns {
			have[c.Name()] = true
		}
		for _, f := range stmt.Schema.Fields {
			if f.DBName != "" && !have[f.DBName] {
				t.Errorf("table %s has no column %s", stmt.Schema.Table, f.DBName)
			}
			delete(have, f.DBName)
		}
		for extra := range have {
			t.Errorf("table %s has column %s that no record reads", stmt.Schema.Table, extra)
		}
	}
}

// probeV1 and probeV2 are two versions of one table.
type probeV1 struct {
	A int `gorm:"column:a"`
}

func (probeV1) TableName() string { return "probe" }

type probeV2 struct {
	A int    `gorm:"column:a"`
	B string `gorm:"column:b"`
}

func (probeV2) TableName() string { return "probe" }

func openBare(t *testing.T) *DB {
	t.Helper()
	db, err := Open(ctx(t), InMemory, WithClock(fakeClock()))
	check(t, err)
	t.Cleanup(func() { check(t, db.Close()) })
	return db
}

func probeMigrations() (first, second migration) {
	first = migration{version: 1, name: "first", up: func(tx *gorm.DB) error { return tx.Migrator().CreateTable(&probeV1{}) }}
	second = migration{version: 2, name: "second", up: func(tx *gorm.DB) error { return tx.Migrator().AddColumn(&probeV2{}, "B") }}
	return first, second
}

func TestMigrationsApplyInOrderAndKeepGoing(t *testing.T) {
	first, second := probeMigrations()
	db := openBare(t)
	check(t, db.applyMigrations(ctx(t), []migration{first}))
	if v, _ := db.SchemaVersion(ctx(t)); v != 1 {
		t.Errorf("version = %d", v)
	}
	// A later run applies only what is new.
	check(t, db.applyMigrations(ctx(t), []migration{first, second}))
	if v, _ := db.SchemaVersion(ctx(t)); v != 2 {
		t.Errorf("version = %d", v)
	}
	if !db.gorm.Migrator().HasColumn(&probeV2{}, "B") {
		t.Error("second migration not applied")
	}

	// The list is applied in version order whatever order it is given in:
	// the second migration needs the table of the first.
	other := openBare(t)
	check(t, other.applyMigrations(ctx(t), []migration{second, first}))
	if v, _ := other.SchemaVersion(ctx(t)); v != 2 {
		t.Errorf("version = %d", v)
	}
}

func TestMigrationFailureRollsBack(t *testing.T) {
	first, _ := probeMigrations()
	bad := migration{version: 2, name: "bad", up: func(tx *gorm.DB) error {
		if err := tx.Migrator().CreateTable(&half{}); err != nil {
			return err
		}
		return errors.New("Boom", "the migration fails halfway")
	}}
	db := openBare(t)
	err := db.applyMigrations(ctx(t), []migration{first, bad})
	wantRoute(t, err, "applyOne", "MigrationFailed")
	if v, _ := db.SchemaVersion(ctx(t)); v != 1 {
		t.Errorf("version = %d, want 1 (the failed migration must not be recorded)", v)
	}
	if db.gorm.Migrator().HasTable(&half{}) {
		t.Error("the half-applied table survived the rollback")
	}
}

type half struct {
	A int `gorm:"column:a"`
}

func TestMigrationsRejectBadVersions(t *testing.T) {
	noop := func(*gorm.DB) error { return nil }
	tests := []struct {
		name string
		ms   []migration
	}{
		{"zero", []migration{{version: 0, name: "x", up: noop}}},
		{"negative", []migration{{version: -1, name: "x", up: noop}}},
		{"duplicate", []migration{{version: 1, name: "a", up: noop}, {version: 1, name: "b", up: noop}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantRoute(t, openBare(t).applyMigrations(ctx(t), tt.ms), "applyMigrations", "MigrationFailed")
		})
	}
}

func TestMigrationNumbersAreContiguous(t *testing.T) {
	ms := migrations()
	check(t, checkMigrations(ms))
	for i, m := range ms {
		if m.version != i+1 {
			t.Errorf("migration %q has version %d at position %d: versions must be contiguous", m.name, m.version, i+1)
		}
	}
}

// serverWithPath is a row of the servers table as it was before migration 4.
type serverWithPath struct {
	ID                int64     `gorm:"column:id;primaryKey;autoIncrement"`
	Name              string    `gorm:"column:name"`
	Host              string    `gorm:"column:host"`
	CodeName          string    `gorm:"column:code_name"`
	IsLocal           bool      `gorm:"column:is_local"`
	MaxConcurrentJobs int       `gorm:"column:max_concurrent_jobs"`
	ComfyUIPath       string    `gorm:"column:comfyui_path"`
	CreatedAt         time.Time `gorm:"column:created_at"`
	UpdatedAt         time.Time `gorm:"column:updated_at"`
}

func (serverWithPath) TableName() string { return "servers" }

// A database made before migration 4 has a ComfyUI folder for its servers;
// the migration drops the column and keeps the servers.
func TestMigrationFourDropsTheComfyUIPathOfServers(t *testing.T) {
	db := openBare(t)
	all := migrations()
	check(t, db.applyMigrations(ctx(t), all[:3]))
	if !db.gorm.Migrator().HasColumn(&v2Server{}, "ComfyUIPath") {
		t.Fatal("migration 2 did not add the column")
	}
	now := fakeClock()()
	check(t, db.gorm.Create(&serverWithPath{Name: "old", Host: "http://old:8188", CodeName: "old", MaxConcurrentJobs: 1, ComfyUIPath: "/opt/ComfyUI", CreatedAt: now, UpdatedAt: now}).Error)

	check(t, db.Migrate(ctx(t)))
	if db.gorm.Migrator().HasColumn(&v2Server{}, "ComfyUIPath") {
		t.Error("the column is still there")
	}
	got, err := db.Repos().Servers.GetByCode(ctx(t), "old")
	check(t, err)
	if got.Name != "old" || got.Host != "http://old:8188" || got.MaxConcurrentJobs != 1 {
		t.Errorf("server = %+v", got)
	}
	// The table was built again: its rules must have survived.
	dup := &domain.Server{Name: "x", Host: "http://x:1", CodeName: "old", MaxConcurrentJobs: 1}
	wantRoute(t, db.Repos().Servers.Create(ctx(t), dup), "ServerRepo.Create", "ServerCodeNameTaken")
	zero := &domain.Server{Name: "y", Host: "http://y:1", CodeName: "y", MaxConcurrentJobs: 0}
	if err := db.Repos().Servers.Create(ctx(t), zero); err == nil {
		t.Error("a server that runs no job at a time was accepted")
	}
}
