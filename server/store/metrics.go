package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// RawPoint is one stored raw measurement.
type RawPoint struct {
	TS                         time.Time
	CPU, Mem, Disk, Rx, Tx, L1 float32
}

// InsertRaw stores raw points (partitions must exist); duplicates are ignored.
func (s *Store) InsertRaw(ctx context.Context, serverID string, pts []RawPoint) error {
	if len(pts) == 0 {
		return nil
	}
	n := len(pts)
	ts := make([]time.Time, n)
	cols := make([][]float32, 6)
	for i := range cols {
		cols[i] = make([]float32, n)
	}
	for i, p := range pts {
		ts[i] = p.TS
		cols[0][i], cols[1][i], cols[2][i], cols[3][i], cols[4][i], cols[5][i] = p.CPU, p.Mem, p.Disk, p.Rx, p.Tx, p.L1
	}
	_, err := s.Pool.Exec(ctx, `INSERT INTO metrics_raw (server_id, ts, cpu, mem, disk, rx, tx, load1)
		SELECT $1, * FROM unnest($2::timestamptz[], $3::real[], $4::real[], $5::real[], $6::real[], $7::real[], $8::real[])
		ON CONFLICT DO NOTHING`, serverID, ts, cols[0], cols[1], cols[2], cols[3], cols[4], cols[5])
	return err
}

var metricNames = []string{"cpu", "mem", "disk", "rx", "tx"}

func binExpr(step time.Duration) string {
	return fmt.Sprintf("to_timestamp(floor(extract(epoch FROM ts)/%d)*%d)", int64(step.Seconds()), int64(step.Seconds()))
}

// Aggregate rolls the finer tier up into the coarser one for the window [from, to].
// It is idempotent: existing buckets are recomputed.
func (s *Store) Aggregate(ctx context.Context, src, dst Tier, from, to time.Time) error {
	from = from.Truncate(dst.Step) // never recompute a bucket from a partial window
	if err := s.EnsureRange(ctx, dst, from, to); err != nil {
		return err
	}
	var sel, upd []string
	for _, m := range metricNames {
		if src.Name == "raw" {
			sel = append(sel, fmt.Sprintf("avg(%[1]s), min(%[1]s), max(%[1]s), percentile_cont(0.95) WITHIN GROUP (ORDER BY %[1]s)", m))
		} else {
			sel = append(sel, fmt.Sprintf("avg(%[1]s_avg), min(%[1]s_min), max(%[1]s_max), percentile_cont(0.95) WITHIN GROUP (ORDER BY %[1]s_p95)", m))
		}
		for _, k := range []string{"avg", "min", "max", "p95"} {
			upd = append(upd, fmt.Sprintf("%[1]s_%[2]s = EXCLUDED.%[1]s_%[2]s", m, k))
		}
	}
	samples := "count(*)"
	if src.Name != "raw" {
		samples = "sum(samples)::int"
	}
	q := fmt.Sprintf(`INSERT INTO %s
		SELECT server_id, %s AS b, %s, %s FROM %s WHERE ts >= $1 AND ts < $2 GROUP BY server_id, b
		ON CONFLICT (server_id, ts) DO UPDATE SET samples = EXCLUDED.samples, %s`,
		dst.Table, binExpr(dst.Step), samples, strings.Join(sel, ", "), src.Table, strings.Join(upd, ", "))
	_, err := s.Pool.Exec(ctx, q, from, to)
	return err
}

// HistoryPoint is one bar of a history chart.
type HistoryPoint struct {
	TS      int64   `json:"ts"`
	CPUAvg  float64 `json:"cpu_avg"`
	CPUMax  float64 `json:"cpu_max"`
	MemAvg  float64 `json:"mem_avg"`
	MemMax  float64 `json:"mem_max"`
	DiskAvg float64 `json:"disk_avg"`
	DiskMax float64 `json:"disk_max"`
	RxAvg   float64 `json:"rx_avg"`
	RxMax   float64 `json:"rx_max"`
	TxAvg   float64 `json:"tx_avg"`
	TxMax   float64 `json:"tx_max"`
}

// MetricStats are summary values of one metric over a period.
type MetricStats struct {
	Min float64 `json:"min"`
	Avg float64 `json:"avg"`
	P95 float64 `json:"p95"`
	Max float64 `json:"max"`
}

// HistoryQuery reads bars from a tier. bin is the bar width.
func (s *Store) HistoryQuery(ctx context.Context, serverID string, tr Tier, from, to time.Time, bin time.Duration) ([]HistoryPoint, map[string]MetricStats, error) {
	var sel []string
	for _, m := range metricNames {
		if tr.Name == "raw" {
			sel = append(sel, fmt.Sprintf("avg(%[1]s), max(%[1]s)", m))
		} else {
			sel = append(sel, fmt.Sprintf("avg(%[1]s_avg), max(%[1]s_max)", m))
		}
	}
	q := fmt.Sprintf(`SELECT extract(epoch FROM %s)::bigint AS b, %s FROM %s WHERE server_id=$1 AND ts >= $2 AND ts <= $3 GROUP BY b ORDER BY b`,
		binExprAs(bin), strings.Join(sel, ", "), tr.Table)
	rows, err := s.Pool.Query(ctx, q, serverID, from, to)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var pts []HistoryPoint
	for rows.Next() {
		var p HistoryPoint
		if err := rows.Scan(&p.TS, &p.CPUAvg, &p.CPUMax, &p.MemAvg, &p.MemMax, &p.DiskAvg, &p.DiskMax, &p.RxAvg, &p.RxMax, &p.TxAvg, &p.TxMax); err != nil {
			return nil, nil, err
		}
		pts = append(pts, p)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	stats := map[string]MetricStats{}
	for _, m := range metricNames {
		var st MetricStats
		var q2 string
		if tr.Name == "raw" {
			q2 = fmt.Sprintf(`SELECT coalesce(min(%[1]s),0), coalesce(avg(%[1]s),0), coalesce(percentile_cont(0.95) WITHIN GROUP (ORDER BY %[1]s),0), coalesce(max(%[1]s),0) FROM %[2]s WHERE server_id=$1 AND ts >= $2 AND ts <= $3`, m, tr.Table)
		} else {
			q2 = fmt.Sprintf(`SELECT coalesce(min(%[1]s_min),0), coalesce(avg(%[1]s_avg),0), coalesce(percentile_cont(0.95) WITHIN GROUP (ORDER BY %[1]s_p95),0), coalesce(max(%[1]s_max),0) FROM %[2]s WHERE server_id=$1 AND ts >= $2 AND ts <= $3`, m, tr.Table)
		}
		if err := s.Pool.QueryRow(ctx, q2, serverID, from, to).Scan(&st.Min, &st.Avg, &st.P95, &st.Max); err != nil {
			return nil, nil, err
		}
		stats[m] = st
	}
	return pts, stats, nil
}

func binExprAs(step time.Duration) string { return binExpr(step) }

// Event is a notable moment on a server's timeline.
type Event struct {
	TS      int64  `json:"ts"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// AddEvent records an event.
func (s *Store) AddEvent(ctx context.Context, serverID string, ts time.Time, kind, msg string) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO events (server_id, ts, kind, message) VALUES ($1,$2,$3,$4)`, serverID, ts, kind, msg)
	return err
}

// Events lists events in a window, newest first (max 300).
func (s *Store) Events(ctx context.Context, serverID string, from, to time.Time) ([]Event, error) {
	rows, err := s.Pool.Query(ctx, `SELECT extract(epoch FROM ts)::bigint, kind, message FROM events
		WHERE server_id=$1 AND ts >= $2 AND ts <= $3 ORDER BY ts DESC LIMIT 300`, serverID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.TS, &e.Kind, &e.Message); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SaveLastState persists the latest full sample and system info of a server.
func (s *Store) SaveLastState(ctx context.Context, serverID string, sample, system []byte) error {
	_, err := s.Pool.Exec(ctx, `UPDATE servers SET last_sample=$2, system_info=coalesce($3, system_info) WHERE id=$1`, serverID, sample, nilIfEmpty(system))
	return err
}

func nilIfEmpty(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

// ServerState is what survives a server restart.
type ServerState struct {
	LastSample []byte
	SystemInfo []byte
}

// LoadLastState reads the persisted sample and system info.
func (s *Store) LoadLastState(ctx context.Context, serverID string) (*ServerState, error) {
	var st ServerState
	err := s.Pool.QueryRow(ctx, `SELECT last_sample, system_info FROM servers WHERE id=$1`, serverID).Scan(&st.LastSample, &st.SystemInfo)
	if err != nil {
		return nil, notFound(err)
	}
	return &st, nil
}

// RecentCPU returns the latest n raw CPU values, oldest first.
func (s *Store) RecentCPU(ctx context.Context, serverID string, n int) ([]float64, error) {
	rows, err := s.Pool.Query(ctx, `SELECT cpu FROM (SELECT cpu, ts FROM metrics_raw WHERE server_id=$1 ORDER BY ts DESC LIMIT $2) x ORDER BY ts`, serverID, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []float64
	for rows.Next() {
		var v float64
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ---- dashboard ----

// DashboardID returns the id of the user's default dashboard.
func (s *Store) DashboardID(ctx context.Context, userID int64) (int64, error) {
	var id int64
	err := s.Pool.QueryRow(ctx, `SELECT id FROM dashboards WHERE user_id=$1 ORDER BY id LIMIT 1`, userID).Scan(&id)
	return id, notFound(err)
}

// DashboardServers lists servers on a dashboard in display order.
func (s *Store) DashboardServers(ctx context.Context, dashID int64) ([]*Server, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+srvColsQ("s")+` FROM dashboard_items i JOIN servers s ON s.id=i.server_id WHERE i.dashboard_id=$1 ORDER BY i.position`, dashID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectServers(rows)
}

// AvailableServers lists servers that are not on the dashboard.
func (s *Store) AvailableServers(ctx context.Context, dashID int64) ([]*Server, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+srvColsQ("s")+` FROM servers s WHERE NOT EXISTS
		(SELECT 1 FROM dashboard_items i WHERE i.dashboard_id=$1 AND i.server_id=s.id) ORDER BY s.name`, dashID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectServers(rows)
}

func srvColsQ(a string) string {
	return fmt.Sprintf(`%[1]s.id::text, %[1]s.name, %[1]s.hostname, %[1]s.address, %[1]s.agent_pubkey, %[1]s.os, %[1]s.agent_version, %[1]s.protocol, %[1]s.enrolled_at, %[1]s.last_seen_at`, a)
}

func collectServers(rows pgx.Rows) ([]*Server, error) {
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

// DashboardAdd appends a server to the dashboard (no-op if already present).
func (s *Store) DashboardAdd(ctx context.Context, dashID int64, serverID string) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO dashboard_items (dashboard_id, server_id, position)
		VALUES ($1, $2, coalesce((SELECT max(position)+1 FROM dashboard_items WHERE dashboard_id=$1), 0)) ON CONFLICT DO NOTHING`, dashID, serverID)
	return err
}

// DashboardRemove removes a server from the dashboard only.
func (s *Store) DashboardRemove(ctx context.Context, dashID int64, serverID string) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM dashboard_items WHERE dashboard_id=$1 AND server_id=$2`, dashID, serverID)
	return err
}

// DashboardSetOrder stores the display order; ids not on the dashboard are ignored.
func (s *Store) DashboardSetOrder(ctx context.Context, dashID int64, ids []string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for i, id := range ids {
		if _, err := tx.Exec(ctx, `UPDATE dashboard_items SET position=$3 WHERE dashboard_id=$1 AND server_id=$2`, dashID, id, i); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// SetModules stores the modules an agent reported in its hello.
func (s *Store) SetModules(ctx context.Context, serverID string, modules []string) error {
	if modules == nil {
		modules = []string{}
	}
	_, err := s.Pool.Exec(ctx, `UPDATE servers SET modules=$2 WHERE id=$1`, serverID, modules)
	return err
}

// Modules returns the modules an agent last reported.
func (s *Store) Modules(ctx context.Context, serverID string) ([]string, error) {
	var m []string
	err := s.Pool.QueryRow(ctx, `SELECT coalesce(ARRAY(SELECT jsonb_array_elements_text(modules)), '{}') FROM servers WHERE id=$1`, serverID).Scan(&m)
	return m, notFound(err)
}

// ---- drafts and pending changes ----

// SaveDraft stores a draft payload.
func (s *Store) SaveDraft(ctx context.Context, serverID, kind string, payload []byte) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO drafts (server_id, kind, payload) VALUES ($1,$2,$3)
		ON CONFLICT (server_id, kind) DO UPDATE SET payload=EXCLUDED.payload, updated_at=now()`, serverID, kind, payload)
	return err
}

// Draft returns a stored draft or ErrNotFound.
func (s *Store) Draft(ctx context.Context, serverID, kind string) ([]byte, error) {
	var b []byte
	err := s.Pool.QueryRow(ctx, `SELECT payload FROM drafts WHERE server_id=$1 AND kind=$2`, serverID, kind).Scan(&b)
	return b, notFound(err)
}

// DeleteDraft removes a draft.
func (s *Store) DeleteDraft(ctx context.Context, serverID, kind string) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM drafts WHERE server_id=$1 AND kind=$2`, serverID, kind)
	return err
}

// Pending is a change waiting for confirmation.
type Pending struct {
	Kind     string
	ChangeID string
	Deadline time.Time
}

// SetPending records a pending change (one per server).
func (s *Store) SetPending(ctx context.Context, serverID string, p Pending) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO pending_changes (server_id, kind, change_id, deadline) VALUES ($1,$2,$3,$4)
		ON CONFLICT (server_id) DO UPDATE SET kind=EXCLUDED.kind, change_id=EXCLUDED.change_id, deadline=EXCLUDED.deadline`,
		serverID, p.Kind, p.ChangeID, p.Deadline)
	return err
}

// PendingFor returns the pending change of a server, if any.
func (s *Store) PendingFor(ctx context.Context, serverID string) (*Pending, error) {
	var p Pending
	err := s.Pool.QueryRow(ctx, `SELECT kind, change_id, deadline FROM pending_changes WHERE server_id=$1`, serverID).Scan(&p.Kind, &p.ChangeID, &p.Deadline)
	if err != nil {
		return nil, notFound(err)
	}
	return &p, nil
}

// ClearPending forgets the pending change.
func (s *Store) ClearPending(ctx context.Context, serverID string) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM pending_changes WHERE server_id=$1`, serverID)
	return err
}
