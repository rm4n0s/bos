package store

import (
	"context"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

// ServerRepo stores ComfyUI servers (SPEC §3.1).
type ServerRepo struct{ base }

func (r *ServerRepo) taken(err error, code string) error {
	if isUnique(err) {
		return errors.New("ServerCodeNameTaken", "a server with this code name already exists", "code_name", code)
	}
	return storeFailure(err, "write server")
}

// Create stores a new server and sets its ID and timestamps.
func (r *ServerRepo) Create(ctx context.Context, v *domain.Server) error {
	now := r.now()
	rec := serverToRecord(v)
	rec.CreatedAt, rec.UpdatedAt = now, now
	if err := r.conn(ctx).Create(&rec).Error; err != nil {
		return r.taken(err, v.CodeName)
	}
	v.ID, v.CreatedAt, v.UpdatedAt = rec.ID, now, now
	return nil
}

// Update replaces the editable fields of a server.
func (r *ServerRepo) Update(ctx context.Context, v *domain.Server) error {
	now := r.now()
	rec := serverToRecord(v)
	rec.UpdatedAt = now
	res := updateRow(r.conn(ctx), &rec, v.ID)
	if res.Error != nil {
		return r.taken(res.Error, v.CodeName)
	}
	if err := affected(res, "write server", "ServerNotFound", "server", v.ID); err != nil {
		return err
	}
	v.UpdatedAt = now
	return nil
}

// Delete removes a server. Jobs keep the host they ran on.
func (r *ServerRepo) Delete(ctx context.Context, id domain.ID) error {
	return affected(r.conn(ctx).Delete(&serverRecord{}, "id = ?", id), "delete server", "ServerNotFound", "server", id)
}

// Get returns one server.
func (r *ServerRepo) Get(ctx context.Context, id domain.ID) (*domain.Server, error) {
	rec, err := find[serverRecord](r.conn(ctx).Where("id = ?", id), "ServerNotFound", "server", id, "read server")
	if err != nil {
		return nil, err
	}
	v := rec.domain()
	return &v, nil
}

// GetByCode returns the server with a code name.
func (r *ServerRepo) GetByCode(ctx context.Context, code string) (*domain.Server, error) {
	var rec serverRecord
	if err := r.conn(ctx).Where("code_name = ?", code).Take(&rec).Error; err != nil {
		if isMissing(err) {
			return nil, errors.New("ServerNotFound", "server not found", "code_name", code)
		}
		return nil, storeFailure(err, "read server")
	}
	v := rec.domain()
	return &v, nil
}

// All returns every server in creation order.
func (r *ServerRepo) All(ctx context.Context) ([]domain.Server, error) {
	recs, err := all[serverRecord](r.conn(ctx).Order("id"), "list servers")
	if err != nil {
		return nil, err
	}
	return mapAll(recs, serverRecord.domain), nil
}

// List returns one page of servers in creation order.
func (r *ServerRepo) List(ctx context.Context, p domain.Page) (domain.Paged[domain.Server], error) {
	return pageOf(r.conn(ctx), "id", p, serverRecord.domain, "list servers")
}
