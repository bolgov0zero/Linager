package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Enrollment is a pending (or finished) agent pairing request.
type Enrollment struct {
	ID           string
	AgentPubkey  []byte
	Hostname     string
	Address      string
	RemoteAddr   string
	OS           string
	AgentVersion string
	Protocol     int
	Nonce        []byte
	Attempts     int
	Status       string
	CertPEM      string
	ServerID     string
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

const enrCols = `id, agent_pubkey, hostname, address, remote_addr, os, agent_version, protocol, nonce, attempts, status, coalesce(cert_pem,''), coalesce(server_id::text,''), created_at, expires_at`

func scanEnr(r pgx.Row) (*Enrollment, error) {
	var e Enrollment
	err := r.Scan(&e.ID, &e.AgentPubkey, &e.Hostname, &e.Address, &e.RemoteAddr, &e.OS, &e.AgentVersion, &e.Protocol, &e.Nonce, &e.Attempts, &e.Status, &e.CertPEM, &e.ServerID, &e.CreatedAt, &e.ExpiresAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &e, nil
}

// CreateEnrollment registers a pairing request; an existing pending request for the same key is replaced.
func (s *Store) CreateEnrollment(ctx context.Context, e *Enrollment, ttl time.Duration) (*Enrollment, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DELETE FROM enrollments WHERE agent_pubkey=$1 AND status='pending'`, e.AgentPubkey); err != nil {
		return nil, err
	}
	out, err := scanEnr(tx.QueryRow(ctx, `INSERT INTO enrollments (agent_pubkey,hostname,address,remote_addr,os,agent_version,protocol,nonce,expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,now()+make_interval(secs => $9)) RETURNING `+enrCols,
		e.AgentPubkey, e.Hostname, e.Address, e.RemoteAddr, e.OS, e.AgentVersion, e.Protocol, e.Nonce, ttl.Seconds()))
	if err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

// Enrollment loads one request by id.
func (s *Store) Enrollment(ctx context.Context, id string) (*Enrollment, error) {
	return scanEnr(s.Pool.QueryRow(ctx, `SELECT `+enrCols+` FROM enrollments WHERE id=$1`, id))
}

// PendingEnrollments lists unexpired pending requests.
func (s *Store) PendingEnrollments(ctx context.Context) ([]*Enrollment, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+enrCols+` FROM enrollments WHERE status='pending' AND expires_at>now() ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Enrollment
	for rows.Next() {
		e, err := scanEnr(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// BumpEnrollmentAttempts increments failed key attempts; after max the request is rejected.
func (s *Store) BumpEnrollmentAttempts(ctx context.Context, id string, max int) (int, error) {
	var n int
	err := s.Pool.QueryRow(ctx, `UPDATE enrollments SET attempts=attempts+1,
		status = CASE WHEN attempts+1 >= $2 THEN 'rejected' ELSE status END
		WHERE id=$1 AND status='pending' RETURNING attempts`, id, max).Scan(&n)
	return n, notFound(err)
}

// RejectEnrollment marks a pending request as rejected.
func (s *Store) RejectEnrollment(ctx context.Context, id string) error {
	ct, err := s.Pool.Exec(ctx, `UPDATE enrollments SET status='rejected' WHERE id=$1 AND status='pending'`, id)
	if err == nil && ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// Server is an enrolled agent host.
type Server struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Hostname     string     `json:"hostname"`
	Address      string     `json:"address"`
	AgentPubkey  []byte     `json:"-"`
	OS           string     `json:"os"`
	AgentVersion string     `json:"agent_version"`
	Protocol     int        `json:"protocol"`
	EnrolledAt   time.Time  `json:"enrolled_at"`
	LastSeenAt   *time.Time `json:"last_seen_at"`
}

const srvCols = `id::text, name, hostname, address, agent_pubkey, os, agent_version, protocol, enrolled_at, last_seen_at`

func scanSrv(r pgx.Row) (*Server, error) {
	var v Server
	if err := r.Scan(&v.ID, &v.Name, &v.Hostname, &v.Address, &v.AgentPubkey, &v.OS, &v.AgentVersion, &v.Protocol, &v.EnrolledAt, &v.LastSeenAt); err != nil {
		return nil, notFound(err)
	}
	return &v, nil
}

// ApproveEnrollment atomically creates the server, stores the issued certificate and
// marks the request approved. issue is called with the new server id inside the transaction.
func (s *Store) ApproveEnrollment(ctx context.Context, e *Enrollment, issue func(serverID string) (certPEM string, err error)) (*Server, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	name := uniqueName(ctx, tx, e.Hostname)
	srv, err := scanSrv(tx.QueryRow(ctx, `INSERT INTO servers (name,hostname,address,agent_pubkey,os,agent_version,protocol)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING `+srvCols, name, e.Hostname, e.Address, e.AgentPubkey, e.OS, e.AgentVersion, e.Protocol))
	if err != nil {
		return nil, err
	}
	cert, err := issue(srv.ID)
	if err != nil {
		return nil, err
	}
	ct, err := tx.Exec(ctx, `UPDATE enrollments SET status='approved', cert_pem=$2, server_id=$3 WHERE id=$1 AND status='pending'`, e.ID, cert, srv.ID)
	if err != nil {
		return nil, err
	}
	if ct.RowsAffected() != 1 {
		return nil, ErrNotFound
	}
	if _, err := tx.Exec(ctx, `INSERT INTO dashboard_items (dashboard_id, server_id, position)
		SELECT d.id, $1, coalesce((SELECT max(position)+1 FROM dashboard_items WHERE dashboard_id=d.id),0) FROM dashboards d`, srv.ID); err != nil {
		return nil, err
	}
	return srv, tx.Commit(ctx)
}

func uniqueName(ctx context.Context, tx pgx.Tx, base string) string {
	name := base
	for i := 2; ; i++ {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM servers WHERE name=$1`, name).Scan(&n); err != nil || n == 0 {
			return name
		}
		name = base + "-" + itoa(i)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}

// ServerByID loads a server.
func (s *Store) ServerByID(ctx context.Context, id string) (*Server, error) {
	return scanSrv(s.Pool.QueryRow(ctx, `SELECT `+srvCols+` FROM servers WHERE id=$1`, id))
}

// Servers lists all servers.
func (s *Store) Servers(ctx context.Context) ([]*Server, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+srvCols+` FROM servers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Server
	for rows.Next() {
		v, err := scanSrv(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// DeleteServer removes a server and, by cascade, its dashboard items and commands.
// Its certificate stops working immediately because connections are checked against this table.
func (s *Store) DeleteServer(ctx context.Context, id string) error {
	ct, err := s.Pool.Exec(ctx, `DELETE FROM servers WHERE id=$1`, id)
	if err == nil && ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// TouchServer updates last-seen time and reported agent details.
func (s *Store) TouchServer(ctx context.Context, id, os, agentVersion string, protocol int) error {
	_, err := s.Pool.Exec(ctx, `UPDATE servers SET last_seen_at=now(), os=coalesce(nullif($2,''),os),
		agent_version=coalesce(nullif($3,''),agent_version), protocol=$4 WHERE id=$1`, id, os, agentVersion, protocol)
	return err
}
