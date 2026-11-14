package store

import "context"

// IgnoredServices lists the units whose failures do not count towards the server status.
func (s *Store) IgnoredServices(ctx context.Context, serverID string) ([]string, error) {
	rows, err := s.Pool.Query(ctx, `SELECT unit FROM ignored_services WHERE server_id=$1 ORDER BY unit`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// SetServiceIgnored adds or removes a unit from the ignore list.
func (s *Store) SetServiceIgnored(ctx context.Context, serverID, unit string, ignore bool) error {
	var err error
	if ignore {
		_, err = s.Pool.Exec(ctx, `INSERT INTO ignored_services (server_id, unit) VALUES ($1, $2) ON CONFLICT DO NOTHING`, serverID, unit)
	} else {
		_, err = s.Pool.Exec(ctx, `DELETE FROM ignored_services WHERE server_id=$1 AND unit=$2`, serverID, unit)
	}
	return err
}
