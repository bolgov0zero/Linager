package store

import (
	"context"
	"encoding/json"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"
)

// The gateway module: the configuration of an agent's scanning and the devices it found.

// GatewayMissedGone is how many passes in a row a device may not answer before it is reported as gone: one lost
// packet must not raise an alarm.
const GatewayMissedGone = 2

// GatewayNetLast is the result of the last pass of a network.
type GatewayNetLast struct {
	PassID     string `json:"pass_id"`
	FinishedAt int64  `json:"finished_at"`
	Probed     int    `json:"probed"`
	Answered   int    `json:"answered"`
	Found      int    `json:"found"`
	Error      string `json:"error,omitempty"`
}

// GatewayNet is a network of the gateway as the panel keeps it.
type GatewayNet struct {
	CIDR        string          `json:"cidr"`
	On          bool            `json:"on"`
	Ports       bool            `json:"ports"`
	Names       bool            `json:"names"`
	Public      bool            `json:"public"` // a public network the administrator confirmed
	ConfirmedBy string          `json:"confirmed_by,omitempty"`
	ConfirmedAt int64           `json:"confirmed_at,omitempty"`
	Own         bool            `json:"own"`
	Iface       string          `json:"iface,omitempty"`
	Via         string          `json:"via,omitempty"`
	Scanned     bool            `json:"scanned"` // a pass has finished here at least once, so a new device is news
	Last        *GatewayNetLast `json:"last,omitempty"`
}

// GatewayCfg is the configuration of the gateway of one agent.
type GatewayCfg struct {
	ServerID  string
	Enabled   bool
	IntervalS int
	Nets      []GatewayNet
	UpdatedAt time.Time
	UpdatedBy string
	DataAt    *time.Time
}

// GatewayConfig returns the configuration of an agent (scanning off, no networks when none was saved).
func (s *Store) GatewayConfig(ctx context.Context, serverID string) (*GatewayCfg, error) {
	c := &GatewayCfg{ServerID: serverID, IntervalS: 300, Nets: []GatewayNet{}}
	var raw []byte
	err := s.Pool.QueryRow(ctx, `SELECT enabled, interval_s, nets, updated_at, updated_by, data_at FROM gateways WHERE server_id=$1`, serverID).
		Scan(&c.Enabled, &c.IntervalS, &raw, &c.UpdatedAt, &c.UpdatedBy, &c.DataAt)
	if err != nil {
		if err == pgx.ErrNoRows { //nolint:errorlint // pgx returns the sentinel itself
			return c, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(raw, &c.Nets); err != nil {
		return nil, err
	}
	if c.Nets == nil {
		c.Nets = []GatewayNet{}
	}
	return c, nil
}

// SetGatewayConfig saves the configuration of an agent.
func (s *Store) SetGatewayConfig(ctx context.Context, c *GatewayCfg) error {
	nets, err := json.Marshal(c.Nets)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `INSERT INTO gateways (server_id, enabled, interval_s, nets, updated_at, updated_by) VALUES ($1,$2,$3,$4,now(),$5)
		ON CONFLICT (server_id) DO UPDATE SET enabled=EXCLUDED.enabled, interval_s=EXCLUDED.interval_s, nets=EXCLUDED.nets, updated_at=now(), updated_by=EXCLUDED.updated_by`,
		c.ServerID, c.Enabled, c.IntervalS, nets, c.UpdatedBy)
	return err
}

// GatewayPort is an open port of a device.
type GatewayPort struct {
	Port   int    `json:"port"`
	Banner string `json:"banner,omitempty"`
}

// GatewayUPnP is the UPnP description of a device.
type GatewayUPnP struct {
	Type  string `json:"type,omitempty"`
	Model string `json:"model,omitempty"`
	Maker string `json:"maker,omitempty"`
}

// GatewayInfo is what a device says about itself besides its ports and names.
type GatewayInfo struct {
	Svc    []string     `json:"svc,omitempty"`
	UPnP   *GatewayUPnP `json:"upnp,omitempty"`
	Router bool         `json:"router,omitempty"`
}

// GatewayDevice is a device found by an agent.
type GatewayDevice struct {
	ID        int64
	Net       string
	IP        string
	MAC       string
	Name      string
	NameSrc   string
	OwnName   string
	Vendor    string
	RandomMAC bool
	Ports     []GatewayPort
	Via       []string
	FirstSeen time.Time
	LastSeen  time.Time
	Online    bool
	Excluded  bool
	Initial   bool
	Info      GatewayInfo
}

// GatewayEvent is a device that appeared or disappeared.
type GatewayEvent struct {
	ID     int64
	TS     time.Time
	Kind   string
	IP     string
	Name   string
	Vendor string
	Net    string
}

// GatewayObs is what one pass saw of one device, with the name and the manufacturer already chosen.
type GatewayObs struct {
	IP        netip.Addr
	MAC       string
	Name      string
	NameSrc   string
	Vendor    string
	RandomMAC bool
	Ports     []GatewayPort
	Via       []string
	Info      GatewayInfo
}

// GatewayPassIn is the result of one network of a pass.
type GatewayPassIn struct {
	PassID     string
	CIDR       string
	Own        bool
	Iface      string
	Via        string
	FinishedAt int64
	Probed     int
	Answered   int
	Err        string
	Devices    []GatewayObs
}

func devLabel(own, name string) string {
	if own != "" {
		return own
	}
	return name
}

// GatewayIngest stores one network of a pass: devices that answered are inserted or refreshed, devices of the
// network that did not are counted as missed (and reported as gone after GatewayMissedGone passes), and the network
// gets the summary of the pass. A pass it has seen already (a replay after a reconnect) changes nothing. Events of
// appearing are written only for a network that has been scanned before: the first pass finds everything at once.
// It reports whether the configuration of the agent knows the network.
func (s *Store) GatewayIngest(ctx context.Context, serverID string, in GatewayPassIn) (bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// one pass at a time per agent
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT nets FROM gateways WHERE server_id=$1 FOR UPDATE`, serverID).Scan(&raw); err != nil {
		if err == pgx.ErrNoRows { //nolint:errorlint // pgx returns the sentinel itself
			return false, nil
		}
		return false, err
	}
	var nets []GatewayNet
	if err := json.Unmarshal(raw, &nets); err != nil {
		return false, err
	}
	idx := -1
	for i := range nets {
		if nets[i].CIDR == in.CIDR {
			idx = i
		}
	}
	if idx < 0 {
		return false, nil
	}
	n := &nets[idx]
	if n.Last != nil && n.Last.PassID == in.PassID {
		return true, nil // a replay
	}
	last := &GatewayNetLast{PassID: in.PassID, FinishedAt: in.FinishedAt, Probed: in.Probed, Answered: in.Answered, Error: in.Err}
	n.Own, n.Iface, n.Via = in.Own, in.Iface, in.Via

	if in.Err == "" {
		seen := make([]int64, 0, len(in.Devices))
		for _, o := range in.Devices {
			id, err := ingestDevice(ctx, tx, serverID, in.CIDR, n.Scanned, o)
			if err != nil {
				return false, err
			}
			seen = append(seen, id)
		}
		rows, err := tx.Query(ctx, `UPDATE gateway_devices SET missed=missed+1, online=(missed+1 < $4)
			WHERE server_id=$1 AND net=$2 AND online AND NOT (id = ANY($3))
			RETURNING host(ip), name, own_name, vendor, online`, serverID, in.CIDR, seen, GatewayMissedGone)
		if err != nil {
			return false, err
		}
		type gone struct{ ip, name, vendor string }
		var goneList []gone
		for rows.Next() {
			var ip, name, own, vendor string
			var online bool
			if err := rows.Scan(&ip, &name, &own, &vendor, &online); err != nil {
				rows.Close()
				return false, err
			}
			if !online {
				goneList = append(goneList, gone{ip, devLabel(own, name), vendor})
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return false, err
		}
		for _, g := range goneList {
			if _, err := tx.Exec(ctx, `INSERT INTO gateway_events (server_id, kind, ip, name, vendor, net) VALUES ($1,'disappeared',$2::inet,$3,$4,$5)`,
				serverID, g.ip, g.name, g.vendor, in.CIDR); err != nil {
				return false, err
			}
		}
		last.Found = len(in.Devices)
		n.Scanned = true
	}
	n.Last = last
	out, err := json.Marshal(nets)
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE gateways SET nets=$2, data_at=now() WHERE server_id=$1`, serverID, out); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func ingestDevice(ctx context.Context, tx pgx.Tx, serverID, cidr string, scanned bool, o GatewayObs) (int64, error) {
	ports, err := json.Marshal(append([]GatewayPort{}, o.Ports...))
	if err != nil {
		return 0, err
	}
	via := o.Via
	if via == nil {
		via = []string{}
	}
	ip := o.IP.String()
	infoMap := map[string]any{"router": o.Info.Router}
	if len(o.Info.Svc) > 0 {
		infoMap["svc"] = o.Info.Svc
	}
	if o.Info.UPnP != nil {
		infoMap["upnp"] = o.Info.UPnP
	}
	info, err := json.Marshal(infoMap)
	if err != nil {
		return 0, err
	}
	var id int64
	var wasOnline bool
	var found bool
	if o.MAC != "" {
		err = tx.QueryRow(ctx, `SELECT id, online FROM gateway_devices WHERE server_id=$1 AND mac=$2 LIMIT 1`, serverID, o.MAC).Scan(&id, &wasOnline)
		found = err == nil
	}
	if !found {
		err = tx.QueryRow(ctx, `SELECT id, online FROM gateway_devices WHERE server_id=$1 AND ip=$2::inet AND (mac='' OR mac=$3) ORDER BY last_seen DESC LIMIT 1`,
			serverID, ip, o.MAC).Scan(&id, &wasOnline)
		found = err == nil
	}
	if err != nil && err != pgx.ErrNoRows { //nolint:errorlint // pgx returns the sentinel itself
		return 0, err
	}
	if found {
		_, err = tx.Exec(ctx, `UPDATE gateway_devices SET net=$2, ip=$3::inet,
			mac=CASE WHEN $4<>'' THEN $4 ELSE mac END,
			name=CASE WHEN $5<>'' THEN $5 ELSE name END, name_src=CASE WHEN $5<>'' THEN $6 ELSE name_src END,
			vendor=CASE WHEN $4<>'' THEN $7 ELSE vendor END, random_mac=CASE WHEN $4<>'' THEN $8 ELSE random_mac END,
			ports=$9, via=$10, info=info || $11::jsonb, last_seen=now(), online=true, missed=0 WHERE id=$1`,
			id, cidr, ip, o.MAC, o.Name, o.NameSrc, o.Vendor, o.RandomMAC, ports, via, info)
		if err != nil {
			return 0, err
		}
		if !wasOnline && scanned { // back after it was reported gone
			if err := deviceEvent(ctx, tx, serverID, id, "appeared"); err != nil {
				return 0, err
			}
		}
		return id, nil
	}
	err = tx.QueryRow(ctx, `INSERT INTO gateway_devices (server_id, net, ip, mac, name, name_src, vendor, random_mac, ports, via, initial, info)
		VALUES ($1,$2,$3::inet,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb) RETURNING id`,
		serverID, cidr, ip, o.MAC, o.Name, o.NameSrc, o.Vendor, o.RandomMAC, ports, via, !scanned, info).Scan(&id)
	if err != nil {
		return 0, err
	}
	if scanned {
		if err := deviceEvent(ctx, tx, serverID, id, "appeared"); err != nil {
			return 0, err
		}
	}
	return id, nil
}

func deviceEvent(ctx context.Context, tx pgx.Tx, serverID string, id int64, kind string) error {
	_, err := tx.Exec(ctx, `INSERT INTO gateway_events (server_id, kind, ip, name, vendor, net)
		SELECT server_id, $3, ip, CASE WHEN own_name<>'' THEN own_name ELSE name END, vendor, net FROM gateway_devices WHERE id=$2 AND server_id=$1`,
		serverID, id, kind)
	return err
}

// GatewayDevices lists the devices of an agent in the order of their addresses.
func (s *Store) GatewayDevices(ctx context.Context, serverID string) ([]GatewayDevice, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id, net, host(ip), mac, name, name_src, own_name, vendor, random_mac, ports, via, first_seen, last_seen, online, excluded, initial, info
		FROM gateway_devices WHERE server_id=$1 ORDER BY ip`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GatewayDevice{}
	for rows.Next() {
		var d GatewayDevice
		var ports, info []byte
		if err := rows.Scan(&d.ID, &d.Net, &d.IP, &d.MAC, &d.Name, &d.NameSrc, &d.OwnName, &d.Vendor, &d.RandomMAC, &ports, &d.Via, &d.FirstSeen, &d.LastSeen, &d.Online, &d.Excluded, &d.Initial, &info); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(ports, &d.Ports); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(info, &d.Info); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// GatewayEvents lists the newest events of an agent.
func (s *Store) GatewayEvents(ctx context.Context, serverID string, since time.Time, limit int) ([]GatewayEvent, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id, ts, kind, host(ip), name, vendor, net FROM gateway_events WHERE server_id=$1 AND ts >= $2 ORDER BY ts DESC, id DESC LIMIT $3`, serverID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GatewayEvent{}
	for rows.Next() {
		var e GatewayEvent
		if err := rows.Scan(&e.ID, &e.TS, &e.Kind, &e.IP, &e.Name, &e.Vendor, &e.Net); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// PatchGatewayDevice changes the name given by the administrator and whether the device is left off the map. A nil
// argument is left as it is. The device must belong to the agent.
func (s *Store) PatchGatewayDevice(ctx context.Context, serverID string, id int64, ownName *string, excluded *bool) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE gateway_devices SET own_name=COALESCE($3, own_name), excluded=COALESCE($4, excluded) WHERE id=$1 AND server_id=$2`,
		id, serverID, ownName, excluded)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// PruneGatewayEvents deletes events older than a moment.
func (s *Store) PruneGatewayEvents(ctx context.Context, before time.Time) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM gateway_events WHERE ts < $1`, before)
	return err
}
