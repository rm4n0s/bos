package store

import (
	"reflect"
	"testing"

	"github.com/rm4n0s/errors"
)

var errRollback = errors.New("TestRollback", "undo the transaction")

func TestSettings(t *testing.T) {
	r := newDB(t).Repos()
	got, err := r.Settings.All(ctx(t))
	check(t, err)
	if len(got) != 0 {
		t.Fatalf("a new database has settings: %v", got)
	}

	check(t, r.Settings.Save(ctx(t), map[string]string{"listen_addr": "127.0.0.1:9000", "results_dir": "/srv/results", "civitai_token": "tok"}))
	got, err = r.Settings.All(ctx(t))
	check(t, err)
	want := map[string]string{"listen_addr": "127.0.0.1:9000", "results_dir": "/srv/results", "civitai_token": "tok"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("settings = %v, want %v", got, want)
	}

	// Saving changes the ones named and leaves the others; an empty value
	// removes the setting, so that its default applies again.
	check(t, r.Settings.Save(ctx(t), map[string]string{"listen_addr": "127.0.0.1:9001", "civitai_token": ""}))
	got, err = r.Settings.All(ctx(t))
	check(t, err)
	want = map[string]string{"listen_addr": "127.0.0.1:9001", "results_dir": "/srv/results"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("settings after a change = %v, want %v", got, want)
	}

	// Saving nothing is fine, and so is removing what is not there.
	check(t, r.Settings.Save(ctx(t), nil))
	check(t, r.Settings.Save(ctx(t), map[string]string{"never_set": ""}))
}

// Settings written in a transaction that is rolled back are not kept.
func TestSettingsAreAtomic(t *testing.T) {
	db := newDB(t)
	err := db.InTx(ctx(t), func(r *Repos) error {
		check(t, r.Settings.Save(ctx(t), map[string]string{"a": "1"}))
		return errRollback
	})
	if err == nil {
		t.Fatal("the transaction was not rolled back")
	}
	got, err := db.Repos().Settings.All(ctx(t))
	check(t, err)
	if len(got) != 0 {
		t.Errorf("settings = %v", got)
	}
}

func TestMigrationThreeAddsTheSettingsTable(t *testing.T) {
	db := openBare(t)
	all := migrations()
	check(t, db.applyMigrations(ctx(t), all[:2]))
	if db.gorm.Migrator().HasTable("settings") {
		t.Fatal("the table is there before migration 3")
	}
	check(t, db.Migrate(ctx(t)))
	if !db.gorm.Migrator().HasTable("settings") {
		t.Error("migration 3 did not make the settings table")
	}
	if v, _ := db.SchemaVersion(ctx(t)); v != len(all) {
		t.Errorf("version = %d", v)
	}
}
