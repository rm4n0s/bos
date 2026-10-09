package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/errors"
)

// probe is a table of its own, to show which database a store talks to.
type probe struct {
	X int `gorm:"column:x"`
}

func TestOpenInMemoryIsPrivate(t *testing.T) {
	ctx := context.Background()
	a, err := Open(ctx, InMemory)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Open(ctx, InMemory)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.gorm.Migrator().CreateTable(&probe{}); err != nil {
		t.Fatal(err)
	}
	if err := b.gorm.Migrator().CreateTable(&probe{}); err != nil {
		t.Fatalf("second in-memory database shares the first: %v", err)
	}
	for _, db := range []*DB{a, b} {
		if err := db.Ping(ctx); err != nil {
			t.Error(err)
		}
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}
}

func TestOpenFileKeepsData(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bos.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.gorm.Migrator().CreateTable(&probe{}); err != nil {
		t.Fatal(err)
	}
	if err := db.gorm.Create(&probe{X: 7}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	var got probe
	if err := db.gorm.Take(&got).Error; err != nil || got.X != 7 {
		t.Errorf("x = %d, err = %v; want 7", got.X, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenFailure(t *testing.T) {
	_, err := Open(context.Background(), filepath.Join(t.TempDir(), "missing-dir", "bos.db"))
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute("Open.StoreFailure") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestCloseTwiceFails(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, InMemory)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(ctx); err == nil {
		t.Fatal("ping after close succeeded")
	} else if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("DB.Ping.StoreFailure") {
		t.Errorf("unexpected error: %v", err)
	}
}
