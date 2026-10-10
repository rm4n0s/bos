package store

import (
	"context"

	"gorm.io/gorm/clause"
)

// settingRecord is a row of the settings table.
type settingRecord struct {
	Key   string `gorm:"column:key;primaryKey"`
	Value string `gorm:"column:value"`
}

func (settingRecord) TableName() string { return "settings" }

// SettingsRepo stores the settings of the application (SPEC §2): the ones that
// are not at their default, by name, as text.
type SettingsRepo struct{ base }

// All returns every stored setting.
func (r *SettingsRepo) All(ctx context.Context) (map[string]string, error) {
	var rows []settingRecord
	if err := r.conn(ctx).Find(&rows).Error; err != nil {
		return nil, storeFailure(err, "read settings")
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.Key] = row.Value
	}
	return out, nil
}

// Save writes the settings it is given and leaves the others as they are. An
// empty value removes the setting, so that its default applies again.
func (r *SettingsRepo) Save(ctx context.Context, values map[string]string) error {
	return r.atomically(ctx, func(tx *Repos) error {
		for key, value := range values {
			if value == "" {
				if err := tx.conn(ctx).Delete(&settingRecord{}, "key = ?", key).Error; err != nil {
					return storeFailure(err, "remove setting")
				}
				continue
			}
			row := settingRecord{Key: key, Value: value}
			err := tx.conn(ctx).Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.AssignmentColumns([]string{"value"}),
			}).Create(&row).Error
			if err != nil {
				return storeFailure(err, "write setting")
			}
		}
		return nil
	})
}
