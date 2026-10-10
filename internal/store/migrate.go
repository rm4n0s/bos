package store

import (
	"context"
	"sort"
	"time"

	"github.com/rm4n0s/errors"
	"gorm.io/gorm"
)

// migration is one numbered schema change (SPEC §14.4, D8). It is written with
// gorm's Migrator on structs frozen for that migration, never as SQL text.
type migration struct {
	version int
	name    string
	up      func(tx *gorm.DB) error
}

// migrations lists the schema changes in order. Add a new one at the end with
// the next number; never edit or remove one that was released.
func migrations() []migration {
	return []migration{
		{version: 1, name: "initial schema", up: migrateV1},
		{version: 2, name: "server comfyui path", up: migrateV2},
		{version: 3, name: "settings", up: migrateV3},
	}
}

// migrationRecord is a row of the version table.
type migrationRecord struct {
	Version   int       `gorm:"column:version;primaryKey;autoIncrement:false"`
	Name      string    `gorm:"column:name;not null"`
	AppliedAt time.Time `gorm:"column:applied_at;not null"`
}

func (migrationRecord) TableName() string { return "schema_migrations" }

// checkMigrations verifies that the versions are positive, unique and ordered.
func checkMigrations(ms []migration) error {
	seen := map[int]string{}
	for _, m := range ms {
		if m.version < 1 {
			return errors.New("MigrationFailed", "migration version must be a positive number", "name", m.name)
		}
		if other, dup := seen[m.version]; dup {
			return errors.New("MigrationFailed", "two migrations have the same version", "version", m.version, "names", other+", "+m.name)
		}
		seen[m.version] = m.name
	}
	sort.SliceStable(ms, func(i, j int) bool { return ms[i].version < ms[j].version })
	return nil
}

// Migrate creates the schema, or brings it up to date, by applying the
// migrations that the version table does not list yet, in order. Each one runs
// in its own transaction, so a failure leaves the database at the last version
// that worked.
func (d *DB) Migrate(ctx context.Context) error {
	return d.applyMigrations(ctx, migrations())
}

func (d *DB) applyMigrations(ctx context.Context, ms []migration) error {
	if err := checkMigrations(ms); err != nil {
		return err
	}
	db := d.gorm.WithContext(ctx)
	if err := db.AutoMigrate(&migrationRecord{}); err != nil {
		return errors.NewErr("MigrationFailed", err).SetMetadata("step", "version table")
	}
	var versions []int
	if err := db.Model(&migrationRecord{}).Pluck("version", &versions).Error; err != nil {
		return errors.NewErr("MigrationFailed", err).SetMetadata("step", "read versions")
	}
	applied := make(map[int]bool, len(versions))
	for _, v := range versions {
		applied[v] = true
	}
	for _, m := range ms {
		if applied[m.version] {
			continue
		}
		if err := d.applyOne(db, m); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) applyOne(db *gorm.DB, m migration) error {
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := m.up(tx); err != nil {
			return err
		}
		return tx.Create(&migrationRecord{Version: m.version, Name: m.name, AppliedAt: d.clock().UTC()}).Error
	})
	if err != nil {
		return errors.NewErr("MigrationFailed", err).SetMetadata("version", m.version, "name", m.name)
	}
	return nil
}

// SchemaVersion returns the highest migration applied, or 0 for an empty
// database.
func (d *DB) SchemaVersion(ctx context.Context) (int, error) {
	var v int
	err := d.gorm.WithContext(ctx).Model(&migrationRecord{}).Select("COALESCE(MAX(version), 0)").Scan(&v).Error
	if err != nil {
		return 0, errors.NewErr("StoreFailure", err).SetMetadata("operation", "schema version")
	}
	return v, nil
}
