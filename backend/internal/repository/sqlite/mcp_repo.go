package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"findus/backend/internal/domain"
)

type MCPRepo struct{ db DBConn }

func NewMCPRepo(db *sql.DB) *MCPRepo { return &MCPRepo{db: db} }

func (r *MCPRepo) GetByUserID(ctx context.Context, userID string) (*domain.MCPAccess, error) {
	return r.get(ctx, `SELECT user_id, token_hash, scopes, created_at FROM mcp_access WHERE user_id = ?`, userID)
}

func (r *MCPRepo) GetByTokenHash(ctx context.Context, hash string) (*domain.MCPAccess, error) {
	return r.get(ctx, `SELECT user_id, token_hash, scopes, created_at FROM mcp_access WHERE token_hash = ?`, hash)
}

func (r *MCPRepo) get(ctx context.Context, query, arg string) (*domain.MCPAccess, error) {
	var a domain.MCPAccess
	var scopes, created string
	err := r.db.QueryRowContext(ctx, query, arg).Scan(&a.UserID, &a.TokenHash, &scopes, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(scopes), &a.Scopes); err != nil {
		return nil, err
	}
	if a.CreatedAt, err = parseTime(created); err != nil {
		return nil, err
	}
	return &a, nil
}

// Save inserts or replaces the single MCP access row of a user.
func (r *MCPRepo) Save(ctx context.Context, a *domain.MCPAccess) error {
	scopes, err := json.Marshal(a.Scopes)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO mcp_access (user_id, token_hash, scopes, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET token_hash = excluded.token_hash, scopes = excluded.scopes, created_at = excluded.created_at`,
		a.UserID, a.TokenHash, string(scopes), formatTime(a.CreatedAt))
	return err
}

func (r *MCPRepo) Delete(ctx context.Context, userID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM mcp_access WHERE user_id = ?`, userID)
	return err
}
