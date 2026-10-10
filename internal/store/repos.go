package store

import (
	"context"
	"time"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
	"gorm.io/gorm"
)

// Repos is the set of repositories over one connection source: the pool, or a
// single transaction.
type Repos struct {
	Servers    *ServerRepo
	Generators *GeneratorRepo
	Fixers     *FixerRepo
	Groups     *GroupRepo
	Items      *ItemRepo
	Tags       *TagRepo
	Projects   *ProjectRepo
	Commands   *CommandRepo
	Jobs       *JobRepo
	Settings   *SettingsRepo

	b base
}

// conn returns the connection source of the repositories, bound to ctx.
func (r *Repos) conn(ctx context.Context) *gorm.DB { return r.b.conn(ctx) }

// Repos returns the repositories on the connection pool. A method that needs
// several statements to be atomic opens its own transaction.
func (d *DB) Repos() *Repos { return newRepos(base{db: d.gorm, clock: d.clock}) }

// InTx runs fn in one transaction on the repositories it is given and commits
// when fn returns nil. It rolls back on an error or a panic. fn must use only
// those repositories: the transaction holds the write lock, and a call through
// the pool would wait for it.
func (d *DB) InTx(ctx context.Context, fn func(r *Repos) error) error {
	err := d.gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(newRepos(base{db: tx, inTx: true, clock: d.clock}))
	})
	if err != nil {
		return asStoreError(err, "transaction")
	}
	return nil
}

func newRepos(b base) *Repos {
	return &Repos{
		Servers:    &ServerRepo{b},
		Generators: &GeneratorRepo{b},
		Fixers:     &FixerRepo{b},
		Groups:     &GroupRepo{b},
		Items:      &ItemRepo{b},
		Tags:       &TagRepo{b},
		Projects:   &ProjectRepo{b},
		Commands:   &CommandRepo{b},
		Jobs:       &JobRepo{b},
		Settings:   &SettingsRepo{b},
		b:          b,
	}
}

// base is what every repository shares: the pool or a transaction, and the
// clock.
type base struct {
	db    *gorm.DB
	inTx  bool
	clock func() time.Time
}

func (b base) now() time.Time { return b.clock().UTC() }

// conn returns the connection source bound to ctx.
func (b base) conn(ctx context.Context) *gorm.DB { return b.db.WithContext(ctx) }

// atomically runs fn in a transaction: the one the repository is already in, or
// a new one when it works on the pool. fn gets the repositories of that
// transaction.
func (b base) atomically(ctx context.Context, fn func(r *Repos) error) error {
	if b.inTx {
		return fn(newRepos(b))
	}
	err := b.conn(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(newRepos(base{db: tx, inTx: true, clock: b.clock}))
	})
	if err != nil {
		return asStoreError(err, "transaction")
	}
	return nil
}

// ---- errors ----

func storeFailure(err error, operation string) *errors.Error {
	return errors.NewErr("StoreFailure", err).SetMetadata("operation", operation)
}

// asStoreError keeps an application error as it is and wraps anything else.
func asStoreError(err error, operation string) error {
	if _, ok := errors.FromError(err); ok {
		return err
	}
	return storeFailure(err, operation)
}

func isUnique(err error) bool { return errors.Is(err, gorm.ErrDuplicatedKey) }

func isForeignKey(err error) bool { return errors.Is(err, gorm.ErrForeignKeyViolated) }

func isMissing(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }

func notFound(tag, what string, id int64) *errors.Error {
	return errors.New(tag, what+" not found", "id", id)
}

// ---- reads and pages ----

// find reads the single row the query selects, as a domain value.
func find[R any](q *gorm.DB, tag, what string, id int64, operation string) (*R, error) {
	var rec R
	if err := q.Take(&rec).Error; err != nil {
		if isMissing(err) {
			return nil, notFound(tag, what, id)
		}
		return nil, storeFailure(err, operation)
	}
	return &rec, nil
}

// all reads every row the query selects.
func all[R any](q *gorm.DB, operation string) ([]R, error) {
	var recs []R
	if err := q.Find(&recs).Error; err != nil {
		return nil, storeFailure(err, operation)
	}
	return recs, nil
}

// mapAll converts records one by one.
func mapAll[R any, D any](recs []R, conv func(R) D) []D {
	out := make([]D, len(recs))
	for i, r := range recs {
		out[i] = conv(r)
	}
	return out
}

// pageOf counts the rows the query selects, then reads one page of them in the
// given order.
func pageOf[R any, D any](q *gorm.DB, order string, p domain.Page, conv func(R) D, operation string) (domain.Paged[D], error) {
	q = q.Model(new(R))
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return domain.Paged[D]{}, storeFailure(err, operation)
	}
	n := p.Normalised()
	recs, err := all[R](q.Session(&gorm.Session{}).Order(order).Limit(n.Size).Offset(n.Offset()), operation)
	if err != nil {
		return domain.Paged[D]{}, err
	}
	return domain.NewPaged(mapAll(recs, conv), int(total), p), nil
}

// updateRow writes every column of a row except its id and creation time.
func updateRow(db *gorm.DB, rec any, id int64) *gorm.DB {
	return db.Model(rec).Where("id = ?", id).Select("*").Omit("id", "created_at").Updates(rec)
}

// affected turns the outcome of an update or a delete into a not-found error
// when no row matched.
func affected(res *gorm.DB, operation, tag, what string, id int64) error {
	if res.Error != nil {
		return storeFailure(res.Error, operation)
	}
	if res.RowsAffected == 0 {
		return notFound(tag, what, id)
	}
	return nil
}

// compact drops empty and repeated paths, keeping the first of each.
func compact(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}
