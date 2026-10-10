package store

import "gorm.io/gorm"

// v2Server is the servers table as migration 2 changes it: a column for the
// folder of the ComfyUI installation. Like the structs of migration 1 it must
// never change; it names only what the migration adds.
type v2Server struct {
	ComfyUIPath string `gorm:"column:comfyui_path;not null;default:''"`
}

func (v2Server) TableName() string { return "servers" }

// migrateV2 adds servers.comfyui_path. Servers that exist keep an empty one.
func migrateV2(tx *gorm.DB) error {
	return tx.Migrator().AddColumn(&v2Server{}, "ComfyUIPath")
}
