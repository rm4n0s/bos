// Package store holds the repositories over SQLite and the schema migrations
// (SPEC §10, §14.2). It uses gorm; the schema is built by numbered Go
// migrations and no SQL is written by hand.
package store

import (
	"context"
	"database/sql"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/rm4n0s/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// InMemory is the database setting that selects a private in-memory database
// (SPEC §2). Every Open call gets its own database.
const InMemory = ":memory:"

// memoryCounter makes the name of each in-memory database unique, so two
// stores opened in one process never share data.
var memoryCounter atomic.Uint64

// DB is an open SQLite database.
type DB struct {
	gorm  *gorm.DB
	pool  *sql.DB
	clock func() time.Time
}

// Option configures Open.
type Option func(*DB)

// WithClock sets the clock used for created_at and updated_at; the default is
// time.Now. Tests pass a fake clock.
func WithClock(now func() time.Time) Option {
	return func(d *DB) { d.clock = now }
}

// Open opens the SQLite database named by location (a file path or InMemory)
// and checks that it answers. It does not create the schema; see Migrate.
func Open(ctx context.Context, location string, opts ...Option) (*DB, error) {
	d := &DB{clock: time.Now}
	for _, opt := range opts {
		opt(d)
	}
	g, err := gorm.Open(sqlite.Open(dsn(location)), &gorm.Config{
		// A repository method that needs several statements opens its own
		// transaction, so gorm need not wrap every single statement in one.
		SkipDefaultTransaction: true,
		// Constraint failures come back as gorm.ErrDuplicatedKey and
		// gorm.ErrForeignKeyViolated, whatever the driver.
		TranslateError: true,
		// The repositories turn a missing row into a tagged error themselves.
		Logger:  logger.Discard,
		NowFunc: func() time.Time { return d.clock().UTC() },
	})
	if err != nil {
		return nil, errors.NewErr("StoreFailure", err).SetMetadata("operation", "open", "location", location)
	}
	pool, err := g.DB()
	if err != nil {
		return nil, errors.NewErr("StoreFailure", err).SetMetadata("operation", "open", "location", location)
	}
	if location == InMemory {
		// An in-memory database lives and dies with its connection, so the pool
		// must hold exactly one and never drop it. One connection also means
		// that statements never contend for locks.
		pool.SetMaxOpenConns(1)
		pool.SetMaxIdleConns(1)
		pool.SetConnMaxLifetime(0)
		pool.SetConnMaxIdleTime(0)
	}
	if err := pool.PingContext(ctx); err != nil {
		closeErr := pool.Close()
		wrapped := errors.NewErr("StoreFailure", err)
		if closeErr != nil {
			return nil, wrapped.SetMetadata("operation", "ping", "location", location, "close_error", closeErr.Error())
		}
		return nil, wrapped.SetMetadata("operation", "ping", "location", location)
	}
	d.gorm, d.pool = g, pool
	return d, nil
}

// dsn builds the driver connection string. Every connection waits for locks
// instead of failing at once, enforces foreign keys and starts its transactions
// with BEGIN IMMEDIATE, so a transaction that will write never fails halfway
// on a lock upgrade. File databases use a write-ahead log so readers do not
// block the writer.
func dsn(location string) string {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Set("_txlock", "immediate")
	if location == InMemory {
		name := "bos-mem-" + strconv.FormatUint(memoryCounter.Add(1), 10)
		q.Set("mode", "memory")
		q.Set("cache", "shared")
		return "file:" + name + "?" + q.Encode()
	}
	q.Add("_pragma", "journal_mode(WAL)")
	return "file:" + strings.TrimPrefix(location, "file:") + "?" + q.Encode()
}

// Ping reports whether the database answers.
func (d *DB) Ping(ctx context.Context) error {
	if err := d.pool.PingContext(ctx); err != nil {
		return errors.NewErr("StoreFailure", err).SetMetadata("operation", "ping")
	}
	return nil
}

// Close releases the database.
func (d *DB) Close() error {
	if err := d.pool.Close(); err != nil {
		return errors.NewErr("StoreFailure", err).SetMetadata("operation", "close")
	}
	return nil
}
