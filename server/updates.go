package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/bolgov0zero/linager/backup"
	"github.com/bolgov0zero/linager/internal/config"
	"github.com/bolgov0zero/linager/internal/protocol"
	"github.com/bolgov0zero/linager/internal/release"
	"github.com/bolgov0zero/linager/internal/version"
	"github.com/bolgov0zero/linager/migrations"
	"github.com/bolgov0zero/linager/server/store"
	"github.com/bolgov0zero/linager/update"
	"github.com/bolgov0zero/linager/updater"
)

// Modes of an agent rollout.
const (
	ModeAll        = "all"
	ModeOneByOne   = "one_by_one"
	ModeFirstRest  = "first_then_rest"
	agentUpdateTTL = 150 * time.Second // agents that did not come back by then are reported as rolled back
)

// Updates owns release discovery, the release cache and agent rollouts.
type Updates struct {
	DB    *store.Store
	Hub   *Hub
	Log   *slog.Logger
	Cfg   *config.Server
	Key   *release.Key
	Src   update.Source
	Cache string // release cache directory

	mu        sync.Mutex
	channel   string
	releases  []update.Release
	checkedAt time.Time
	checkErr  string
	latestMan *release.Manifest
	// checking is closed when the release check in flight ends; checkingCh is the channel it checks.
	checking   chan struct{}
	checkingCh string
	ro         *rollout
	roSaved    string // last persisted rollout state, to skip identical writes
}

// rolloutSetting keeps the running rollout in the settings table, so a server restart (including the
// server's own update) does not strand the agents that are still queued.
const rolloutSetting = "updates.rollout"

type rolloutState struct {
	Mode        string   `json:"mode"`
	StopOnError bool     `json:"stop_on_error"`
	Version     string   `json:"version"`
	Order       []string `json:"order"`
	Started     []string `json:"started"`
	First       string   `json:"first"`
	FirstDone   bool     `json:"first_done"`
	OK          int      `json:"ok"`
	Failed      int      `json:"failed"`
	Rolled      int      `json:"rolled"`
}

// persistRollout stores the running rollout, or clears the record when none is running.
func (u *Updates) persistRollout(ctx context.Context) {
	u.mu.Lock()
	val := ""
	if ro := u.ro; ro != nil && ro.state == "running" {
		b, _ := json.Marshal(rolloutState{Mode: ro.mode, StopOnError: ro.stopOnError, Version: ro.version, Order: ro.order,
			Started: keys(ro.started), First: ro.first, FirstDone: ro.firstDone, OK: ro.ok, Failed: ro.failed, Rolled: ro.rolled})
		val = string(b)
	}
	if val == u.roSaved {
		u.mu.Unlock()
		return
	}
	u.roSaved = val
	u.mu.Unlock()
	_ = u.DB.SetSetting(context.WithoutCancel(ctx), rolloutSetting, val)
}

// restoreRollout continues the rollout that was running when the server stopped.
func (u *Updates) restoreRollout(ctx context.Context) {
	var st rolloutState
	raw := u.DB.Setting(ctx, rolloutSetting)
	rows, _ := u.DB.AgentUpdates(ctx)
	switch {
	case raw != "" && json.Unmarshal([]byte(raw), &st) == nil && st.Version == version.Version:
	case raw != "": // the server itself was updated meanwhile: the old target no longer applies
		u.cancelQueued(ctx)
		_ = u.DB.SetSetting(ctx, rolloutSetting, "")
		return
	default: // queued agents without a stored rollout: start them as they connect
		for id, r := range rows {
			if r.Status == "queued" && r.Version == version.Version {
				st.Order = append(st.Order, id)
			}
		}
		if len(st.Order) == 0 {
			return
		}
		sort.Strings(st.Order)
		st.Mode, st.Version = ModeAll, version.Version
	}
	ro := &rollout{mode: st.Mode, stopOnError: st.StopOnError, version: st.Version, order: st.Order, started: map[string]bool{},
		first: st.First, firstDone: st.FirstDone, state: "running", ok: st.OK, failed: st.Failed, rolled: st.Rolled}
	for _, id := range st.Started {
		ro.started[id] = true
	}
	u.mu.Lock()
	if u.ro == nil {
		u.ro = ro
	}
	u.mu.Unlock()
	u.Log.Info("continuing the agent rollout after a restart", "mode", st.Mode, "agents", len(st.Order))
}

// canaryFailed lets a "first, then the rest" rollout go on after its first agent failed, when the
// administrator chose not to stop on errors. Caller holds u.mu.
func (ro *rollout) canaryFailed(id string) {
	if ro.mode == ModeFirstRest && ro.first == id && !ro.stopOnError {
		ro.firstDone = true
	}
}

type rollout struct {
	mode        string
	stopOnError bool
	version     string
	order       []string        // targets not yet started
	started     map[string]bool // dispatched at least once
	firstDone   bool
	first       string
	state       string // running | done | stopped
	ok, failed  int
	rolled      int
}

// NewUpdates creates the service.
func NewUpdates(db *store.Store, hub *Hub, cfg *config.Server, log *slog.Logger) (*Updates, error) {
	key, err := release.Embedded()
	if err != nil {
		return nil, err
	}
	u := &Updates{DB: db, Hub: hub, Log: log, Cfg: cfg, Key: key, Src: update.NewSource(cfg.Update.Source), Cache: updater.CacheDir(cfg.DataDir), channel: cfg.Update.Channel}
	if c := db.Setting(context.Background(), "updates.channel"); c == update.Beta || c == update.Stable {
		u.channel = c
	}
	return u, nil
}

// Channel returns the active channel.
func (u *Updates) Channel() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.channel
}

// SetChannel switches the channel and refreshes the release list.
func (u *Updates) SetChannel(ctx context.Context, ch string) error {
	if ch != update.Stable && ch != update.Beta {
		return errors.New("unknown channel")
	}
	if err := u.DB.SetSetting(ctx, "updates.channel", ch); err != nil {
		return err
	}
	u.mu.Lock()
	u.channel = ch
	u.mu.Unlock()
	return u.Check(ctx)
}

// Check reads the release list and verifies the manifest of the newest release.
// The network work runs without u.mu, so the updates page, the header counter and the rollout keep
// answering while GitHub is slow. Concurrent checks of the same channel share one request.
func (u *Updates) Check(ctx context.Context) error {
	u.mu.Lock()
	for u.checking != nil {
		wait, sameChannel := u.checking, u.checkingCh == u.channel
		u.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return ctx.Err()
		}
		u.mu.Lock()
		if sameChannel {
			defer u.mu.Unlock()
			if u.checkErr != "" {
				return errors.New(u.checkErr)
			}
			return nil
		}
		// The channel changed while the other check ran: check again for the current one.
	}
	done := make(chan struct{})
	ch := u.channel
	u.checking, u.checkingCh = done, ch
	u.mu.Unlock()

	// The check is shared, so a closed browser tab must not cancel it for the others.
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
	defer cancel()
	rels, err := u.Src.Releases(cctx)
	var man *release.Manifest
	if err == nil {
		if l, ok := update.Latest(rels, ch); ok {
			if m, merr := u.fetchManifest(cctx, l); merr == nil {
				man = m
			}
		}
	}

	u.mu.Lock()
	defer u.mu.Unlock()
	u.checking, u.checkingCh = nil, ""
	close(done)
	u.checkedAt = time.Now()
	if err != nil {
		u.checkErr = err.Error()
		return err
	}
	u.checkErr = ""
	u.releases = rels
	u.latestMan = man
	return nil
}

func (u *Updates) fetchManifest(ctx context.Context, r update.Release) (*release.Manifest, error) {
	if c, err := update.LoadCached(u.Key, u.Cache, r.Tag); err == nil {
		return &c.Manifest, nil
	}
	mb, err := httpBytes(ctx, r.Assets["manifest.json"], 1<<20)
	if err != nil {
		return nil, err
	}
	sb, err := httpBytes(ctx, r.Assets["manifest.json.minisig"], 4096)
	if err != nil {
		return nil, err
	}
	return update.VerifyManifest(u.Key, mb, string(sb))
}

// Latest returns the newest release of the channel (zero Release if none).
func (u *Updates) Latest() (update.Release, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return update.Latest(u.releases, u.channel)
}

// Available returns the version offered for the server, or "" when up to date.
func (u *Updates) Available() string {
	if l, ok := u.Latest(); ok && release.Newer(l.Tag, version.Version) {
		return l.Tag
	}
	return ""
}

// Prefetch downloads and verifies a release into the cache.
func (u *Updates) Prefetch(ctx context.Context, ver string) (*update.Cached, error) {
	if c, err := update.LoadCached(u.Key, u.Cache, ver); err == nil {
		return c, nil
	}
	u.mu.Lock()
	rels := u.releases
	u.mu.Unlock()
	if len(rels) == 0 {
		if err := u.Check(ctx); err != nil {
			return nil, err
		}
		u.mu.Lock()
		rels = u.releases
		u.mu.Unlock()
	}
	rel, err := update.Find(rels, ver)
	if err != nil {
		return nil, err
	}
	dctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	return update.Download(dctx, rel, u.Key, u.Cache)
}

// CurrentRelease returns the cached release matching this server, fetching it if necessary. Agents are
// updated to this exact version.
func (u *Updates) CurrentRelease(ctx context.Context) (*update.Cached, error) {
	if c, err := update.LoadCached(u.Key, u.Cache, version.Version); err == nil {
		return c, nil
	}
	c, err := u.Prefetch(ctx, version.Version)
	if err != nil {
		return nil, errors.New("the release of this server version is not available for agents: " + err.Error())
	}
	return c, nil
}

// ---- server update request ----

// CanUpdateServer reports whether the privileged updater unit is installed.
func (u *Updates) CanUpdateServer() bool {
	_, err := os.Stat("/etc/systemd/system/linager-updater.path")
	return err == nil
}

// RequestServerUpdate hands the update over to the root updater through a request file.
func (u *Updates) RequestServerUpdate(ver, by string) error {
	st := updater.ReadStatus(updater.StatusFile(u.Cfg.DataDir))
	if st.State == "running" {
		return errors.New("an update is already running")
	}
	if !u.CanUpdateServer() {
		return errors.New("the updater service is not installed; run `linager update` on the server")
	}
	b, _ := json.Marshal(updater.Request{Version: ver, Channel: u.Channel(), By: by})
	path := updater.RequestFile(u.Cfg.DataDir)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o640); err != nil { //nolint:gosec // the root updater reads the request, the server group may read it too
		return err
	}
	return os.Rename(tmp, path)
}

// ---- agents ----

// AgentRow is one line of the agents table.
type AgentRow struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	OS         string `json:"os"`
	Address    string `json:"address"`
	Version    string `json:"version"`
	Status     string `json:"status"`
	Online     bool   `json:"online"`
	Target     string `json:"target,omitempty"`
	Step       int    `json:"step,omitempty"`
	StepsTotal int    `json:"steps_total"`
	StepID     string `json:"step_id,omitempty"`
	Message    string `json:"message,omitempty"`
}

var stepIDs = []string{"download", "verify", "backup", "replace", "restart"}

func (u *Updates) agentRows(ctx context.Context) ([]AgentRow, int) {
	servers, _ := u.DB.Servers(ctx)
	rows, _ := u.DB.AgentUpdates(ctx)
	minAgent := ""
	if c, err := update.LoadCached(u.Key, u.Cache, version.Version); err == nil {
		minAgent = c.Manifest.MinAgent
	}
	out := make([]AgentRow, 0, len(servers))
	count := 0
	for _, s := range servers {
		r := AgentRow{ID: s.ID, Name: s.Name, OS: s.OS, Address: s.Address, Version: s.AgentVersion, Online: u.Hub.Online(s.ID), StepsTotal: len(stepIDs)}
		au, has := rows[s.ID]
		switch {
		case has && au.Status == "updating":
			r.Status, r.Target, r.Step, r.Message = "updating", au.Version, au.Step, au.Message
		case has && au.Status == "queued":
			r.Status, r.Target = "queued", au.Version
		case has && au.Status == "rolled_back":
			r.Status, r.Target, r.Message = "rollback", au.Version, au.Message
		case has && au.Status == "failed":
			r.Status, r.Target, r.Message = "failed", au.Version, au.Message
		default:
			r.Status = "current"
			if release.Newer(version.Version, s.AgentVersion) {
				r.Status = "available"
				if minAgent != "" && release.Compare(release.ParseVersion(s.AgentVersion), release.ParseVersion(minAgent)) < 0 {
					r.Status = "unsupported"
				}
			}
			r.Target = version.Version
		}
		if r.Step >= 1 && r.Step <= len(stepIDs) {
			r.StepID = stepIDs[r.Step-1]
		}
		if r.Status == "available" || r.Status == "unsupported" {
			count++
		}
		out = append(out, r)
	}
	return out, count
}

// Summary is the number shown on the "Updates" button: server update plus outdated agents.
func (u *Updates) Summary(ctx context.Context) int {
	_, n := u.agentRows(ctx)
	if u.Available() != "" {
		n++
	}
	return n
}

// RolloutRequest is the body of POST /api/updates/agents.
type RolloutRequest struct {
	ServerIDs   []string `json:"server_ids"`
	All         bool     `json:"all"`
	Mode        string   `json:"mode"`
	StopOnError bool     `json:"stop_on_error"`
}

// StartRollout queues the selected agents and begins dispatching.
func (u *Updates) StartRollout(ctx context.Context, req RolloutRequest) (int, error) {
	if _, err := u.CurrentRelease(ctx); err != nil {
		return 0, err
	}
	rows, _ := u.agentRows(ctx)
	want := map[string]bool{}
	for _, id := range req.ServerIDs {
		want[id] = true
	}
	var order []string
	for _, r := range rows {
		if (req.All || want[r.ID]) && (r.Status == "available" || r.Status == "unsupported" || r.Status == "failed" || r.Status == "rollback") {
			order = append(order, r.ID)
		}
	}
	if len(order) == 0 {
		return 0, errors.New("no agents to update")
	}
	mode := req.Mode
	if mode != ModeOneByOne && mode != ModeFirstRest {
		mode = ModeAll
	}
	u.mu.Lock()
	if u.ro != nil && u.ro.state == "running" {
		u.mu.Unlock()
		return 0, errors.New("a rollout is already running")
	}
	u.ro = &rollout{mode: mode, stopOnError: req.StopOnError, version: version.Version, order: order, started: map[string]bool{}, state: "running"}
	u.mu.Unlock()
	for _, id := range order {
		_ = u.DB.SetAgentUpdate(ctx, store.AgentUpdate{ServerID: id, Version: version.Version, Status: "queued"})
	}
	u.pump(ctx)
	return len(order), nil
}

// RolloutInfo describes the active rollout for the UI.
func (u *Updates) RolloutInfo() map[string]any {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.ro == nil {
		return nil
	}
	return map[string]any{"state": u.ro.state, "mode": u.ro.mode, "stop_on_error": u.ro.stopOnError, "version": u.ro.version}
}

// pump starts as many queued agents as the rollout mode allows.
func (u *Updates) pump(ctx context.Context) {
	u.mu.Lock()
	ro := u.ro
	if ro == nil || ro.state != "running" {
		u.mu.Unlock()
		return
	}
	rows, _ := u.DB.AgentUpdates(ctx)
	running := 0
	for _, id := range append(append([]string(nil), ro.order...), keys(ro.started)...) {
		if rows[id].Status == "updating" {
			running++
		}
	}
	var start []string
	for _, id := range ro.order {
		if ro.started[id] || rows[id].Status != "queued" || !u.Hub.Online(id) {
			continue
		}
		switch ro.mode {
		case ModeAll:
			start = append(start, id)
		case ModeOneByOne:
			if running+len(start) == 0 {
				start = append(start, id)
			}
		case ModeFirstRest:
			if !ro.firstDone && ro.first == "" && running == 0 {
				ro.first = id
				start = append(start, id)
			} else if ro.firstDone {
				start = append(start, id)
			}
		}
	}
	for _, id := range start {
		ro.started[id] = true
	}
	finished := u.rolloutFinished(rows)
	u.mu.Unlock()
	for _, id := range start {
		go u.dispatch(ctx, id)
	}
	if finished {
		u.finishRollout(ctx)
	}
	u.persistRollout(ctx)
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// rolloutFinished reports whether nothing is queued or updating any more. Caller holds u.mu.
func (u *Updates) rolloutFinished(rows map[string]store.AgentUpdate) bool {
	for _, id := range u.ro.order {
		if s := rows[id].Status; s == "queued" || s == "updating" {
			return false
		}
	}
	return true
}

func (u *Updates) finishRollout(ctx context.Context) {
	u.mu.Lock()
	ro := u.ro
	if ro == nil || ro.state != "running" {
		u.mu.Unlock()
		return
	}
	ro.state = "done"
	ok, failed, rolled := ro.ok, ro.failed, ro.rolled
	ver := ro.version
	u.mu.Unlock()
	status := "success"
	if failed > 0 || rolled > 0 {
		status = "failed"
	}
	_ = u.DB.AddUpdateRun(context.WithoutCancel(ctx), store.UpdateRun{Kind: "agents", FromVersion: "", ToVersion: ver, Status: status,
		Detail: itoa(ok) + " ok, " + itoa(failed) + " failed, " + itoa(rolled) + " rolled back"})
}

// dispatch sends the update command to one agent.
func (u *Updates) dispatch(ctx context.Context, id string) {
	ctx = context.WithoutCancel(ctx)
	_ = u.DB.SetAgentUpdate(ctx, store.AgentUpdate{ServerID: id, Version: version.Version, Status: "updating", Step: 1})
	_, err := u.Hub.Call(ctx, id, "update", "apply", map[string]string{"version": version.Version}, true, 4*time.Minute)
	if err != nil {
		var ae *AgentError
		msg := err.Error()
		if errors.As(err, &ae) {
			msg = ae.Msg
		}
		u.fail(ctx, id, msg)
	}
}

func (u *Updates) fail(ctx context.Context, id, msg string) {
	_ = u.DB.SetAgentUpdate(ctx, store.AgentUpdate{ServerID: id, Version: version.Version, Status: "failed", Message: msg})
	u.mu.Lock()
	ro := u.ro
	if ro != nil && ro.state == "running" {
		ro.failed++
		ro.canaryFailed(id)
		if ro.stopOnError {
			ro.state = "stopped"
		}
	}
	stopped := ro != nil && ro.state == "stopped"
	u.mu.Unlock()
	if stopped {
		u.cancelQueued(ctx)
	}
	u.pump(ctx)
}

func (u *Updates) cancelQueued(ctx context.Context) {
	rows, _ := u.DB.AgentUpdates(ctx)
	for id, r := range rows {
		if r.Status == "queued" {
			_ = u.DB.DeleteAgentUpdate(ctx, id)
		}
	}
}

// AgentEvent handles progress messages from an updating agent.
func (u *Updates) AgentEvent(ctx context.Context, id string, e protocol.UpdateEvent) {
	if e.Error != "" {
		u.fail(ctx, id, e.Error)
		return
	}
	_ = u.DB.SetAgentUpdate(ctx, store.AgentUpdate{ServerID: id, Version: e.Version, Status: "updating", Step: e.Step})
}

// AgentHello is called when an agent has connected and reported its version.
func (u *Updates) AgentHello(ctx context.Context, id, agentVersion string) {
	rows, _ := u.DB.AgentUpdates(ctx)
	if r, ok := rows[id]; ok && r.Status == "updating" {
		switch {
		case !release.Newer(r.Version, agentVersion): // agent runs the target version (or newer)
			_ = u.DB.DeleteAgentUpdate(ctx, id)
			u.mu.Lock()
			if u.ro != nil && u.ro.state == "running" {
				u.ro.ok++
				if u.ro.mode == ModeFirstRest && u.ro.first == id {
					u.ro.firstDone = true
				}
			}
			u.mu.Unlock()
		case r.Step >= len(stepIDs): // restarted but still the old version: the agent rolled itself back
			u.markRolledBack(ctx, id, r.Version)
		}
	}
	u.pump(ctx)
}

func (u *Updates) markRolledBack(ctx context.Context, id, ver string) {
	_ = u.DB.SetAgentUpdate(ctx, store.AgentUpdate{ServerID: id, Version: ver, Status: "rolled_back", Message: "agent did not reconnect in time and rolled back"})
	u.mu.Lock()
	if u.ro != nil && u.ro.state == "running" {
		u.ro.rolled++
		u.ro.canaryFailed(id)
		if u.ro.stopOnError {
			u.ro.state = "stopped"
		}
	}
	stopped := u.ro != nil && u.ro.state == "stopped"
	u.mu.Unlock()
	if stopped {
		u.cancelQueued(ctx)
	}
	u.persistRollout(ctx)
}

// Run watches for stuck updates until ctx ends.
func (u *Updates) Run(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	u.restoreRollout(ctx)
	if err := u.Check(ctx); err != nil {
		u.Log.Warn("release check failed", "err", err)
	}
	lastCheck := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			rows, _ := u.DB.AgentUpdates(ctx)
			for id, r := range rows {
				if r.Status == "updating" && time.Since(r.UpdatedAt) > agentUpdateTTL {
					u.markRolledBack(ctx, id, r.Version)
				}
			}
			u.pump(ctx)
			if time.Since(lastCheck) > 6*time.Hour {
				lastCheck = time.Now()
				_ = u.Check(ctx)
			}
		}
	}
}

// ---- overview ----

// Overview builds the data of GET /api/updates.
func (u *Updates) Overview(ctx context.Context) map[string]any {
	u.mu.Lock()
	checked := u.checkedAt
	checkErr := u.checkErr
	man := u.latestMan
	u.mu.Unlock()
	avail := u.Available()
	latest, haveLatest := u.Latest()

	shipped := countMigrations()
	applied, _ := u.DB.AppliedMigrations(ctx)
	pending := 0
	if man != nil && avail != "" && man.Migrations > applied {
		pending = man.Migrations - applied
	}
	notes := []string{}
	if haveLatest && avail != "" {
		notes = update.NotesLines(latest.Notes)
	}
	var lastBackup int64
	if _, ts, err := backup.Last(backup.DefaultDir, "linager-backup"); err == nil {
		lastBackup = ts.Unix()
	}
	if _, ts, err := backup.Last(backup.DefaultDir, "linager-pre-update"); err == nil && ts.Unix() > lastBackup {
		lastBackup = ts.Unix()
	}
	st := updater.ReadStatus(updater.StatusFile(u.Cfg.DataDir))
	srcName := u.Cfg.Update.Source
	if srcName == "" {
		srcName = "github.com/" + update.DefaultRepo
	}
	server := map[string]any{
		"current": version.Version, "available": nilIfEmpty(avail), "checked_at": checked.Unix(), "check_error": checkErr,
		"notes": notes, "db_migrations": map[string]int{"applied": applied, "shipped": shipped, "pending": pending},
		"last_backup": lastBackup, "source": srcName, "can_update": u.CanUpdateServer(),
		"update_run": st,
	}
	cache := []map[string]any{}
	have := map[string]bool{}
	for _, c := range update.ListCached(u.Key, u.Cache) {
		have[c.Version] = true
		cache = append(cache, map[string]any{"version": c.Version, "size": c.Size, "downloaded_at": c.Time.Unix(), "verified": true, "downloaded": true})
	}
	if avail != "" && !have["v"+trimV(avail)] {
		cache = append([]map[string]any{{"version": avail, "size": 0, "downloaded_at": 0, "verified": false, "downloaded": false}}, cache...)
	}
	agents, _ := u.agentRows(ctx)
	hist, _ := u.DB.UpdateRuns(ctx, 20)
	minAgent := ""
	if c, err := update.LoadCached(u.Key, u.Cache, version.Version); err == nil {
		minAgent = c.Manifest.MinAgent
	}
	return map[string]any{"channel": u.Channel(), "server": server, "cache": cache, "agents": agents, "rollout": u.RolloutInfo(),
		"history": hist, "min_agent": minAgent, "agents_target": version.Version, "latest_known": nilIfEmpty(latest.Tag)}
}

func trimV(s string) string {
	if len(s) > 0 && s[0] == 'v' {
		return s[1:]
	}
	return s
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func countMigrations() int {
	n := 0
	ents, _ := migrations.FS.ReadDir(".")
	for _, e := range ents {
		if filepath.Ext(e.Name()) == ".sql" {
			n++
		}
	}
	return n
}
