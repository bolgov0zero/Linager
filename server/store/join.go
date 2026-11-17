package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// JoinUse is one agent that came with a token.
type JoinUse struct {
	Host string `json:"host"`
	At   int64  `json:"at"`
}

// JoinToken is an install link (kind "link") or the one-shot token of an install over SSH (kind "ssh").
type JoinToken struct {
	ID          int64
	Label       string
	Kind        string
	CreatedBy   string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	MaxUses     int // 0 = no limit
	Uses        int
	AutoApprove bool
	Networks    []string
	GroupID     *int64
	Revoked     bool
	Used        []JoinUse
}

// Valid tells whether the token may still be used.
func (t *JoinToken) Valid(now time.Time) bool {
	return !t.Revoked && now.Before(t.ExpiresAt) && (t.MaxUses == 0 || t.Uses < t.MaxUses)
}

const joinCols = `id, label, kind, created_by, created_at, expires_at, max_uses, uses, auto_approve, networks, group_id, revoked, used`

func scanJoin(r pgx.Row) (*JoinToken, error) {
	var t JoinToken
	var used []byte
	if err := r.Scan(&t.ID, &t.Label, &t.Kind, &t.CreatedBy, &t.CreatedAt, &t.ExpiresAt, &t.MaxUses, &t.Uses, &t.AutoApprove, &t.Networks, &t.GroupID, &t.Revoked, &used); err != nil {
		return nil, notFound(err)
	}
	_ = json.Unmarshal(used, &t.Used)
	return &t, nil
}

// CreateJoinToken stores a token (by the hash of its secret) and returns it with its id.
func (s *Store) CreateJoinToken(ctx context.Context, t JoinToken, hash []byte) (*JoinToken, error) {
	if t.Networks == nil {
		t.Networks = []string{}
	}
	return scanJoin(s.Pool.QueryRow(ctx, `INSERT INTO join_tokens (token_hash,label,kind,created_by,expires_at,max_uses,auto_approve,networks,group_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING `+joinCols,
		hash, t.Label, t.Kind, t.CreatedBy, t.ExpiresAt, t.MaxUses, t.AutoApprove, t.Networks, t.GroupID))
}

// JoinTokenByHash finds a token by the hash of its secret, whatever its state.
func (s *Store) JoinTokenByHash(ctx context.Context, hash []byte) (*JoinToken, error) {
	return scanJoin(s.Pool.QueryRow(ctx, `SELECT `+joinCols+` FROM join_tokens WHERE token_hash=$1`, hash))
}

// JoinTokenByID loads a token by id.
func (s *Store) JoinTokenByID(ctx context.Context, id int64) (*JoinToken, error) {
	return scanJoin(s.Pool.QueryRow(ctx, `SELECT `+joinCols+` FROM join_tokens WHERE id=$1`, id))
}

// JoinTokens lists the install links that can still be used, newest first.
func (s *Store) JoinTokens(ctx context.Context) ([]*JoinToken, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+joinCols+` FROM join_tokens
		WHERE kind='link' AND NOT revoked AND expires_at>now() AND (max_uses=0 OR uses<max_uses) ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*JoinToken
	for rows.Next() {
		t, err := scanJoin(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RevokeJoinToken turns a token off.
func (s *Store) RevokeJoinToken(ctx context.Context, id int64) error {
	ct, err := s.Pool.Exec(ctx, `UPDATE join_tokens SET revoked=true WHERE id=$1 AND kind='link'`, id)
	if err == nil && ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// UseJoinToken counts one use of a valid token and remembers who came. It reports false when the token is no
// longer valid, so two agents cannot both take the last use.
func (s *Store) UseJoinToken(ctx context.Context, id int64, host string) (bool, error) {
	ct, err := s.Pool.Exec(ctx, `UPDATE join_tokens SET uses=uses+1,
		used = used || jsonb_build_array(jsonb_build_object('host', $2::text, 'at', $3::bigint))
		WHERE id=$1 AND NOT revoked AND expires_at>now() AND (max_uses=0 OR uses<max_uses)`, id, host, time.Now().Unix())
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() == 1, nil
}

// PurgeJoinTokens deletes tokens that ended more than a day ago.
func (s *Store) PurgeJoinTokens(ctx context.Context) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM join_tokens WHERE expires_at < now() - interval '1 day' OR (revoked AND created_at < now() - interval '1 day')`)
	return err
}

// EndJoinToken turns a token off whatever its kind (the one-shot token of an SSH install ends with the install).
func (s *Store) EndJoinToken(ctx context.Context, id int64) error {
	_, err := s.Pool.Exec(ctx, `UPDATE join_tokens SET revoked=true WHERE id=$1`, id)
	return err
}

// EnrollmentByToken returns the newest enrollment that came with a token.
func (s *Store) EnrollmentByToken(ctx context.Context, tokenID int64) (*Enrollment, error) {
	return scanEnr(s.Pool.QueryRow(ctx, `SELECT `+enrCols+` FROM enrollments WHERE token_id=$1 ORDER BY created_at DESC LIMIT 1`, tokenID))
}
