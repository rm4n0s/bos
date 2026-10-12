package store

import "gorm.io/gorm"

// migrateV4 drops servers.comfyui_path, which migration 2 added. Every ComfyUI
// server runs the comfyui-model-crud extension now, and the program reaches
// the output and models folders through it instead of through a folder on this
// machine. The folders that were stored are discarded.
//
// SQLite cannot drop a column in place, so gorm builds the table again, and the
// table comes back without its indexes: the unique index of the code name is
// made again.
func migrateV4(tx *gorm.DB) error {
	m := tx.Migrator()
	if err := m.DropColumn(&v2Server{}, "ComfyUIPath"); err != nil {
		return err
	}
	if m.HasIndex(&v1Server{}, "servers_code_name") {
		return nil
	}
	return m.CreateIndex(&v1Server{}, "servers_code_name")
}
