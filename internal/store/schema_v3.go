package store

import "gorm.io/gorm"

// v3Setting is the settings table as migration 3 made it: one row for each
// setting that is not at its default, the value as text. Like the structs of
// the other migrations it must never change.
type v3Setting struct {
	Key   string `gorm:"column:key;primaryKey;not null"`
	Value string `gorm:"column:value;not null"`
}

func (v3Setting) TableName() string { return "settings" }

// migrateV3 adds the settings table. The settings used to be a JSON file; they
// are kept in the database now (SPEC §2).
func migrateV3(tx *gorm.DB) error {
	return tx.Migrator().CreateTable(&v3Setting{})
}
