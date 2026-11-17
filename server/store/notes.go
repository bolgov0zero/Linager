package store

import (
	"context"
	"time"
)

// ServerNote is the note of a server.
type ServerNote struct {
	Text      string
	UpdatedAt *time.Time
	UpdatedBy string
}

// ServerNote returns the note of a server (empty when there is none).
func (s *Store) ServerNote(ctx context.Context, serverID string) (ServerNote, error) {
	var n ServerNote
	err := s.Pool.QueryRow(ctx, `SELECT note, note_updated_at, note_updated_by FROM servers WHERE id=$1`, serverID).Scan(&n.Text, &n.UpdatedAt, &n.UpdatedBy)
	return n, notFound(err)
}

// SetServerNote replaces the note of a server and remembers who did it and when.
func (s *Store) SetServerNote(ctx context.Context, serverID, text, by string) (ServerNote, error) {
	var n ServerNote
	err := s.Pool.QueryRow(ctx, `UPDATE servers SET note=$2, note_updated_at=now(), note_updated_by=$3 WHERE id=$1
		RETURNING note, note_updated_at, note_updated_by`, serverID, text, by).Scan(&n.Text, &n.UpdatedAt, &n.UpdatedBy)
	return n, notFound(err)
}
