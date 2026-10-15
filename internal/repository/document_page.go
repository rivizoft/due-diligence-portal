package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rivizoft/due-diligence-portal/internal/domain"
)

// DocumentFilter scopes both the result count and the returned page. An empty
// ViewerID is reserved for staff; investors use the same grant rules as delivery.
type DocumentFilter struct {
	CategoryID string
	Query      string
	ViewerID   string
	OpenAccess bool
}

func (r *documentRepository) Find(ctx context.Context, filter DocumentFilter, limit, offset int) ([]*domain.Document, int, error) {
	from := ` FROM documents d`
	where := []string{"d.is_archived = 0"}
	args := []any{}
	order := "d.created_at DESC, d.id DESC"
	if filter.CategoryID != "" {
		where = append(where, "d.category_id = ?")
		args = append(args, filter.CategoryID)
	}
	if filter.Query != "" {
		match := ftsQuery(filter.Query)
		if match == "" {
			return []*domain.Document{}, 0, nil
		}
		from += ` JOIN documents_fts ON documents_fts.rowid = d.rowid`
		where = append(where, "documents_fts MATCH ?")
		args = append(args, match)
		order = "rank, d.id"
	}
	if filter.ViewerID != "" {
		matched := `SELECT 1 FROM access_grants ag WHERE ag.user_id = ? AND
			((ag.resource_type = 'document' AND ag.resource_id = d.id) OR
			 (ag.resource_type = 'category' AND ag.resource_id = d.category_id))`
		active := matched + ` AND (ag.expires_at IS NULL OR ag.expires_at > ?)
			AND ag.access_level IN ('view','download','upload','manage')`
		visible := "EXISTS (" + active + ")"
		if filter.OpenAccess {
			// In open mode, matching explicit grants still constrain access.
			visible = "NOT EXISTS (" + matched + ") OR " + visible
			args = append(args, filter.ViewerID)
		}
		where = append(where, "("+visible+")")
		args = append(args, filter.ViewerID, time.Now().UTC().Format(time.RFC3339))
	}
	predicate := " WHERE " + strings.Join(where, " AND ")
	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*)"+from+predicate, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count visible documents: %w", err)
	}
	query := `SELECT d.id, d.name, d.description, d.category_id, d.uploaded_by, d.current_version,
		d.mime_type, d.file_size, d.is_archived, d.tags, d.created_at, d.updated_at,
		COALESCE(c.name,''), COALESCE(u.name,'')` + from +
		` LEFT JOIN categories c ON c.id = d.category_id LEFT JOIN users u ON u.id = d.uploaded_by` +
		predicate + " ORDER BY " + order + " LIMIT ? OFFSET ?"
	args = append(args, limit, offset)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("find visible documents: %w", err)
	}
	defer rows.Close()
	var docs []*domain.Document
	for rows.Next() {
		d, err := r.scanDocumentRow(rows)
		if err != nil {
			return nil, 0, err
		}
		docs = append(docs, d)
	}
	return docs, total, rows.Err()
}
