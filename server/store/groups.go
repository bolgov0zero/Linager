package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrGroupExists is returned when a group with the same name (case-insensitive) already exists.
var ErrGroupExists = errors.New("group name already in use")

// Group is a named set of servers shown as a filter on the dashboard.
type Group struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func groupErr(err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return ErrGroupExists
	}
	return err
}

// Groups lists groups in creation order.
func (s *Store) Groups(ctx context.Context) ([]Group, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id, name FROM server_groups ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Group{}
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// GroupCreate adds a group.
func (s *Store) GroupCreate(ctx context.Context, name string) (Group, error) {
	g := Group{Name: name}
	err := s.Pool.QueryRow(ctx, `INSERT INTO server_groups (name) VALUES ($1) RETURNING id`, name).Scan(&g.ID)
	return g, groupErr(err)
}

// GroupRename renames a group; ErrNotFound if it does not exist.
func (s *Store) GroupRename(ctx context.Context, id int64, name string) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE server_groups SET name=$2 WHERE id=$1`, id, name)
	if err != nil {
		return groupErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GroupDelete removes a group; its servers stay, without a group.
func (s *Store) GroupDelete(ctx context.Context, id int64) (string, error) {
	var name string
	err := s.Pool.QueryRow(ctx, `DELETE FROM server_groups WHERE id=$1 RETURNING name`, id).Scan(&name)
	return name, notFound(err)
}

// GroupName returns the name of a group or ErrNotFound.
func (s *Store) GroupName(ctx context.Context, id int64) (string, error) {
	var name string
	err := s.Pool.QueryRow(ctx, `SELECT name FROM server_groups WHERE id=$1`, id).Scan(&name)
	return name, notFound(err)
}

// SetServerGroup puts a server into a group (nil = no group).
func (s *Store) SetServerGroup(ctx context.Context, serverID string, groupID *int64) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE servers SET group_id=$2 WHERE id=$1`, serverID, groupID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ServerGroups maps server id to group id for servers that are in a group.
func (s *Store) ServerGroups(ctx context.Context) (map[string]int64, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id::text, group_id FROM servers WHERE group_id IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var id string
		var g int64
		if err := rows.Scan(&id, &g); err != nil {
			return nil, err
		}
		out[id] = g
	}
	return out, rows.Err()
}
