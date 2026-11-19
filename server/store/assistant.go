package store

import (
	"context"
	"encoding/json"

	"github.com/bolgov0zero/linager/internal/assist"
)

// AssistantSettings returns the picture settings of a server's Assistant (the defaults until they are changed).
func (s *Store) AssistantSettings(ctx context.Context, serverID string) (assist.Settings, error) {
	var raw []byte
	if err := s.Pool.QueryRow(ctx, `SELECT assistant FROM servers WHERE id=$1`, serverID).Scan(&raw); err != nil {
		return assist.Settings{}, notFound(err)
	}
	set := assist.DefaultSettings()
	if len(raw) > 2 { // "{}" is the default
		var got assist.Settings
		if json.Unmarshal(raw, &got) == nil && got.Validate() == nil {
			set = got
		}
	}
	return set, nil
}

// SetAssistantSettings stores the picture settings of a server's Assistant.
func (s *Store) SetAssistantSettings(ctx context.Context, serverID string, set assist.Settings) error {
	raw, err := json.Marshal(set)
	if err != nil {
		return err
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE servers SET assistant=$2 WHERE id=$1`, serverID, raw)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
