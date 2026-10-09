package store

import (
	"context"
	"database/sql"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

// ServerRepo stores ComfyUI servers (SPEC §3.1).
type ServerRepo struct{ base }

const serverColumns = `id, name, host, code_name, is_local, max_concurrent_jobs, created_at, updated_at`

func scanServer(s scanner) (*domain.Server, error) {
	var v domain.Server
	var local int
	var created, updated string
	if err := s.Scan(&v.ID, &v.Name, &v.Host, &v.CodeName, &local, &v.MaxConcurrentJobs, &created, &updated); err != nil {
		return nil, err
	}
	v.IsLocal = local != 0
	var err error
	if v.CreatedAt, err = parseTime(created); err != nil {
		return nil, err
	}
	if v.UpdatedAt, err = parseTime(updated); err != nil {
		return nil, err
	}
	return &v, nil
}

func (r *ServerRepo) taken(err error, code string) error {
	if isUnique(err) {
		return errors.New("ServerCodeNameTaken", "a server with this code name already exists", "code_name", code)
	}
	return storeFailure(err, "write server")
}

// Create stores a new server and sets its ID and timestamps.
func (r *ServerRepo) Create(ctx context.Context, v *domain.Server) error {
	now := r.now()
	res, err := r.q.ExecContext(ctx,
		`INSERT INTO servers (name, host, code_name, is_local, max_concurrent_jobs, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		v.Name, v.Host, v.CodeName, boolInt(v.IsLocal), v.MaxConcurrentJobs, formatTime(now), formatTime(now))
	if err != nil {
		return r.taken(err, v.CodeName)
	}
	if v.ID, err = res.LastInsertId(); err != nil {
		return storeFailure(err, "server id")
	}
	v.CreatedAt, v.UpdatedAt = now, now
	return nil
}

// Update replaces the editable fields of a server.
func (r *ServerRepo) Update(ctx context.Context, v *domain.Server) error {
	now := r.now()
	res, err := r.q.ExecContext(ctx,
		`UPDATE servers SET name=?, host=?, code_name=?, is_local=?, max_concurrent_jobs=?, updated_at=? WHERE id=?`,
		v.Name, v.Host, v.CodeName, boolInt(v.IsLocal), v.MaxConcurrentJobs, formatTime(now), v.ID)
	if err != nil {
		return r.taken(err, v.CodeName)
	}
	if n, err := res.RowsAffected(); err != nil {
		return storeFailure(err, "server rows")
	} else if n == 0 {
		return notFound("ServerNotFound", "server", v.ID)
	}
	v.UpdatedAt = now
	return nil
}

// Delete removes a server. Jobs keep the host they ran on.
func (r *ServerRepo) Delete(ctx context.Context, id domain.ID) error {
	res, err := r.q.ExecContext(ctx, `DELETE FROM servers WHERE id=?`, id)
	if err != nil {
		return storeFailure(err, "delete server")
	}
	if n, err := res.RowsAffected(); err != nil {
		return storeFailure(err, "server rows")
	} else if n == 0 {
		return notFound("ServerNotFound", "server", id)
	}
	return nil
}

// Get returns one server.
func (r *ServerRepo) Get(ctx context.Context, id domain.ID) (*domain.Server, error) {
	v, err := scanServer(r.q.QueryRowContext(ctx, `SELECT `+serverColumns+` FROM servers WHERE id=?`, id))
	return one(v, err, "ServerNotFound", "server", id, "read server")
}

// GetByCode returns the server with a code name.
func (r *ServerRepo) GetByCode(ctx context.Context, code string) (*domain.Server, error) {
	v, err := scanServer(r.q.QueryRowContext(ctx, `SELECT `+serverColumns+` FROM servers WHERE code_name=?`, code))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("ServerNotFound", "server not found", "code_name", code)
	}
	if err != nil {
		return nil, asStoreError(err, "read server")
	}
	return v, nil
}

// All returns every server in creation order.
func (r *ServerRepo) All(ctx context.Context) ([]domain.Server, error) {
	return queryAll(ctx, r.q, `SELECT `+serverColumns+` FROM servers ORDER BY id`, scanServer, "list servers")
}

// List returns one page of servers in creation order.
func (r *ServerRepo) List(ctx context.Context, p domain.Page) (domain.Paged[domain.Server], error) {
	return queryPage(ctx, r.q, `FROM servers`, nil, `ORDER BY id`, serverColumns, scanServer, p, "list servers")
}

// one turns the result of a single-row read into the value or the right error.
func one[T any](v *T, err error, tag, what string, id int64, operation string) (*T, error) {
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound(tag, what, id)
	}
	if err != nil {
		return nil, asStoreError(err, operation)
	}
	return v, nil
}

// asStoreError keeps an application error as it is and wraps anything else.
func asStoreError(err error, operation string) error {
	if _, ok := errors.FromError(err); ok {
		return err
	}
	return storeFailure(err, operation)
}

// queryAll runs a query and scans every row.
func queryAll[T any](ctx context.Context, q dbtx, query string, scan func(scanner) (*T, error), operation string, args ...any) ([]T, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storeFailure(err, operation)
	}
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, closeRows(rows, asStoreError(err, operation))
		}
		out = append(out, *v)
	}
	if err := rows.Err(); err != nil {
		return nil, closeRows(rows, storeFailure(err, operation))
	}
	if err := rows.Close(); err != nil {
		return nil, storeFailure(err, operation)
	}
	return out, nil
}

// queryPage counts the rows that match, then reads one page of them. from is
// the FROM clause with its WHERE, order the ORDER BY.
func queryPage[T any](ctx context.Context, q dbtx, from string, args []any, order, columns string,
	scan func(scanner) (*T, error), p domain.Page, operation string) (domain.Paged[T], error) {
	var total int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) `+from, args...).Scan(&total); err != nil {
		return domain.Paged[T]{}, storeFailure(err, operation)
	}
	limit, offset := pageArgs(p)
	items, err := queryAll(ctx, q, `SELECT `+columns+` `+from+` `+order+` LIMIT ? OFFSET ?`, scan, operation,
		append(append([]any(nil), args...), limit, offset)...)
	if err != nil {
		return domain.Paged[T]{}, err
	}
	return domain.NewPaged(items, total, p), nil
}
