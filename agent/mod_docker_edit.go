//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Editing a container. The panel asks for the editable settings of a container ("docker.config") and sends
// the whole wanted state back ("docker.edit", a stream with the log of the work). The agent compares it with
// what the container has now and decides how to apply it:
//   - name, restart policy and the limits of memory and CPU are changed in place (docker rename / update),
//     the container keeps running;
//   - the image, the command, ports, mounts and variables need a new container: the same replacement as
//     the image update (swapContainer): the old one is stopped and renamed, the new one is created, started
//     and checked, and if anything fails the old one comes back.
//
// A container of a compose project may only change what is applied in place (and not its name): the rest is
// the business of the compose file and would be lost at the next update of the project.

const miB = 1 << 20

var (
	envNameRe   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	volumeRe    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
	safeArgRe   = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)
	restartKind = map[string]bool{"no": true, "always": true, "unless-stopped": true, "on-failure": true}
)

type editPort struct {
	IP    string `json:"ip"`
	Host  int    `json:"host"`
	Cont  int    `json:"cont"`
	Proto string `json:"proto"`
}

type editMount struct {
	Type string `json:"type"` // bind or volume
	Src  string `json:"src"`
	Dst  string `json:"dst"`
	RO   bool   `json:"ro"`
}

type editEnv struct {
	K string `json:"k"`
	V string `json:"v"`
}

// editSpec is the editable state of a container; the same shape goes both ways.
type editSpec struct {
	Name    string      `json:"name"`
	Image   string      `json:"image"`
	Restart string      `json:"restart"`
	Retries int         `json:"retries"`
	Cmd     string      `json:"cmd"`
	MemMB   int         `json:"mem_mb"`
	CPUs    float64     `json:"cpus"`
	Ports   []editPort  `json:"ports"`
	Mounts  []editMount `json:"mounts"`
	Env     []editEnv   `json:"env"`
}

type editConfig struct {
	editSpec
	Running bool         `json:"running"`
	Compose *editCompose `json:"compose,omitempty"`
}

type editCompose struct {
	Project string `json:"project"`
	File    string `json:"file"`
}

// ---- command line: split and join (the shell rules the panel shows) ----

func splitCmd(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	has := false
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n':
			if has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		case c == '\'':
			has = true
			i++
			for i < len(rs) && rs[i] != '\'' {
				cur.WriteRune(rs[i])
				i++
			}
			if i >= len(rs) {
				return nil, errors.New("the command has an unclosed quote")
			}
		case c == '"':
			has = true
			i++
			for i < len(rs) && rs[i] != '"' {
				if rs[i] == '\\' && i+1 < len(rs) && (rs[i+1] == '"' || rs[i+1] == '\\') {
					i++
				}
				cur.WriteRune(rs[i])
				i++
			}
			if i >= len(rs) {
				return nil, errors.New("the command has an unclosed quote")
			}
		case c == '\\' && i+1 < len(rs):
			has = true
			i++
			cur.WriteRune(rs[i])
		default:
			has = true
			cur.WriteRune(c)
		}
	}
	if has {
		out = append(out, cur.String())
	}
	return out, nil
}

func joinCmd(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		if safeArgRe.MatchString(a) {
			parts[i] = a
		} else {
			parts[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(parts, " ")
}

// ---- reading the current state ----

func jnum(v any) float64 {
	f, _ := v.(float64)
	return f
}

func jstr(v any) string {
	s, _ := v.(string)
	return s
}

func jstrs(v any) []string {
	var out []string
	if l, ok := v.([]any); ok {
		for _, x := range l {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func wildcardIP(ip string) bool { return ip == "" || ip == "0.0.0.0" || ip == "::" }

// currentSpec reads the editable settings from the inspect data; imgEnv are the variables the image sets itself
// (they are not shown, Docker adds them to every container).
func currentSpec(old *dInspect, imgEnv []string) editSpec {
	sp := editSpec{Name: strings.TrimPrefix(old.Name, "/"), Image: jstr(old.Config["Image"]), Ports: []editPort{}, Mounts: []editMount{}, Env: []editEnv{}}
	rp, _ := old.HostConfig["RestartPolicy"].(map[string]any)
	sp.Restart = jstr(rp["Name"])
	if sp.Restart == "" {
		sp.Restart = "no"
	}
	sp.Retries = int(jnum(rp["MaximumRetryCount"]))
	if sp.Restart != "on-failure" {
		sp.Retries = 0
	}
	sp.Cmd = joinCmd(jstrs(old.Config["Cmd"]))
	sp.MemMB = int(math.Ceil(jnum(old.HostConfig["Memory"]) / miB))
	sp.CPUs = math.Round(jnum(old.HostConfig["NanoCpus"])/1e6) / 1000
	pb, _ := old.HostConfig["PortBindings"].(map[string]any)
	keys := make([]string, 0, len(pb))
	for k := range pb {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		cont, proto, _ := strings.Cut(k, "/")
		cp, err := strconv.Atoi(cont)
		if err != nil {
			continue
		}
		bl, _ := pb[k].([]any)
		for _, b := range bl {
			bm, _ := b.(map[string]any)
			hp, err := strconv.Atoi(jstr(bm["HostPort"]))
			if err != nil || hp == 0 {
				continue // a random or a range host port is kept as it is and not shown
			}
			ip := jstr(bm["HostIp"])
			if ip == "" {
				ip = "0.0.0.0"
			}
			sp.Ports = append(sp.Ports, editPort{IP: ip, Host: hp, Cont: cp, Proto: proto})
		}
	}
	for _, m := range old.Mounts {
		switch {
		case m.Type == "bind":
			sp.Mounts = append(sp.Mounts, editMount{Type: "bind", Src: m.Source, Dst: m.Destination, RO: !m.RW})
		case m.Type == "volume" && len(m.Name) != 64: // anonymous volumes are not shown, they stay as they are
			sp.Mounts = append(sp.Mounts, editMount{Type: "volume", Src: m.Name, Dst: m.Destination, RO: !m.RW})
		}
	}
	skip := map[string]bool{}
	for _, e := range imgEnv {
		skip[e] = true
	}
	for _, e := range jstrs(old.Config["Env"]) {
		if skip[e] {
			continue
		}
		k, v, _ := strings.Cut(e, "=")
		sp.Env = append(sp.Env, editEnv{K: k, V: v})
	}
	return sp
}

func imageEnv(ctx context.Context, imageID string) []string {
	var im struct {
		Config struct{ Env []string }
	}
	if dockerJSON(ctx, http.MethodGet, "/images/"+url.PathEscape(imageID)+"/json", &im) != nil {
		return nil
	}
	return im.Config.Env
}

// dockerConfig is the command "docker.config": {id} -> the editable settings.
func dockerConfig(ctx context.Context, raw json.RawMessage, _ bool) (any, error) {
	var p struct{ ID string }
	if err := json.Unmarshal(raw, &p); err != nil || !containerRe.MatchString(p.ID) {
		return nil, errors.New("invalid container")
	}
	var old dInspect
	if err := dockerJSON(ctx, http.MethodGet, "/containers/"+p.ID+"/json", &old); err != nil {
		return nil, err
	}
	cfg := editConfig{editSpec: currentSpec(&old, imageEnv(ctx, old.Image)), Running: old.State.Running}
	if l := stringMap(old.Config["Labels"]); l[composeProject] != "" {
		cfg.Compose = &editCompose{Project: l[composeProject], File: first(strings.Split(l["com.docker.compose.project.config_files"], ","))}
	}
	return cfg, nil
}

// ---- the wanted state: normalizing, checking, planning ----

func normalizeSpec(sp editSpec) editSpec {
	sp.Name, sp.Image, sp.Cmd = strings.TrimSpace(sp.Name), strings.TrimSpace(sp.Image), strings.TrimSpace(sp.Cmd)
	if sp.Restart == "" {
		sp.Restart = "no"
	}
	if sp.Restart != "on-failure" {
		sp.Retries = 0
	}
	ports := make([]editPort, 0, len(sp.Ports))
	for _, p := range sp.Ports {
		p.IP, p.Proto = strings.TrimSpace(p.IP), strings.ToLower(strings.TrimSpace(p.Proto))
		if p.IP == "" {
			p.IP = "0.0.0.0"
		}
		if p.Proto == "" {
			p.Proto = "tcp"
		}
		ports = append(ports, p)
	}
	sp.Ports = ports
	if sp.Mounts == nil {
		sp.Mounts = []editMount{}
	}
	if sp.Env == nil {
		sp.Env = []editEnv{}
	}
	return sp
}

// validateSpec checks the wanted state; it does not look at the host.
func validateSpec(sp editSpec) error {
	if !containerRe.MatchString(sp.Name) {
		return errors.New("invalid container name")
	}
	if !imageRe.MatchString(sp.Image) {
		return errors.New("invalid image name")
	}
	if !restartKind[sp.Restart] {
		return errors.New("invalid restart policy")
	}
	if sp.Retries < 0 || sp.Retries > 1000 {
		return errors.New("invalid number of restart attempts")
	}
	if len(sp.Cmd) > 8192 {
		return errors.New("the command is too long")
	}
	if _, err := splitCmd(sp.Cmd); err != nil {
		return err
	}
	if sp.MemMB < 0 || (sp.MemMB > 0 && sp.MemMB < 6) || sp.MemMB > 1<<24 {
		return errors.New("invalid memory limit (at least 6 MB)")
	}
	if math.IsNaN(sp.CPUs) || sp.CPUs < 0 || sp.CPUs > 1024 {
		return errors.New("invalid CPU limit")
	}
	if len(sp.Ports) > 200 || len(sp.Mounts) > 200 || len(sp.Env) > 500 {
		return errors.New("too many ports, mounts or variables")
	}
	for i, p := range sp.Ports {
		if p.Host < 1 || p.Host > 65535 || p.Cont < 1 || p.Cont > 65535 {
			return fmt.Errorf("port %d: the numbers must be from 1 to 65535", p.Host)
		}
		if p.Proto != "tcp" && p.Proto != "udp" {
			return fmt.Errorf("port %d: the protocol is tcp or udp", p.Host)
		}
		if net.ParseIP(p.IP) == nil {
			return fmt.Errorf("port %d: invalid address %q", p.Host, p.IP)
		}
		for _, q := range sp.Ports[:i] {
			if q.Host == p.Host && q.Proto == p.Proto && (wildcardIP(q.IP) || wildcardIP(p.IP) || q.IP == p.IP) {
				return fmt.Errorf("port %d/%s is listed twice", p.Host, p.Proto)
			}
		}
	}
	dsts := map[string]bool{}
	for _, m := range sp.Mounts {
		if m.Type != "bind" && m.Type != "volume" {
			return errors.New("invalid mount type")
		}
		for _, s := range []string{m.Src, m.Dst} {
			if s == "" || strings.ContainsAny(s, ":\x00\r\n,") {
				return fmt.Errorf("invalid mount path %q", s)
			}
		}
		if m.Type == "bind" && !strings.HasPrefix(m.Src, "/") {
			return fmt.Errorf("the path %q on the server must be absolute", m.Src)
		}
		if m.Type == "volume" && !volumeRe.MatchString(m.Src) {
			return fmt.Errorf("invalid volume name %q", m.Src)
		}
		if !strings.HasPrefix(m.Dst, "/") {
			return fmt.Errorf("the path %q in the container must be absolute", m.Dst)
		}
		if dsts[m.Dst] {
			return fmt.Errorf("the path %s is used by two mounts", m.Dst)
		}
		dsts[m.Dst] = true
	}
	names := map[string]bool{}
	for _, e := range sp.Env {
		if !envNameRe.MatchString(e.K) {
			return fmt.Errorf("invalid variable name %q", e.K)
		}
		if names[e.K] {
			return fmt.Errorf("the variable %s is listed twice", e.K)
		}
		names[e.K] = true
		if len(e.V) > 32768 || strings.ContainsRune(e.V, 0) {
			return fmt.Errorf("invalid value of %s", e.K)
		}
	}
	return nil
}

type editPlan struct {
	changed  []string // what differs: name, image, restart, cmd, memory, cpus, ports, mounts, env
	recreate bool
}

func (p editPlan) has(k string) bool {
	for _, c := range p.changed {
		if c == k {
			return true
		}
	}
	return false
}

func cmdArgs(s string) []string {
	a, _ := splitCmd(s)
	if a == nil {
		a = []string{}
	}
	return a
}

// planEdit compares the wanted state with the current one. compose is the project of a compose container.
func planEdit(cur, want editSpec, compose string) (editPlan, error) {
	var p editPlan
	add := func(k string) { p.changed = append(p.changed, k) }
	if cur.Name != want.Name {
		add("name")
	}
	if cur.Image != want.Image {
		add("image")
		p.recreate = true
	}
	if cur.Restart != want.Restart || cur.Retries != want.Retries {
		add("restart")
	}
	if !reflect.DeepEqual(cmdArgs(cur.Cmd), cmdArgs(want.Cmd)) {
		add("cmd")
		p.recreate = true
	}
	if cur.MemMB != want.MemMB {
		add("memory")
		if want.MemMB == 0 {
			p.recreate = true // a limit cannot be taken off a running container in place
		}
	}
	if math.Abs(cur.CPUs-want.CPUs) > 0.0005 {
		add("cpus")
		if want.CPUs == 0 {
			p.recreate = true
		}
	}
	if !reflect.DeepEqual(cur.Ports, want.Ports) {
		add("ports")
		p.recreate = true
	}
	if !reflect.DeepEqual(cur.Mounts, want.Mounts) {
		add("mounts")
		p.recreate = true
	}
	if !reflect.DeepEqual(cur.Env, want.Env) {
		add("env")
		p.recreate = true
	}
	if compose != "" && (p.recreate || p.has("name")) {
		return p, fmt.Errorf("the container belongs to the compose project %s: only the restart policy and the limits can be changed here, the rest is set in the compose file", compose)
	}
	return p, nil
}

// ---- applying ----

func memSwapFor(old *dInspect, bytes int64) int64 {
	if jnum(old.HostConfig["MemorySwap"]) == -1 {
		return -1
	}
	return 2 * bytes
}

// buildEditBody makes the "create" request: the old configuration with the wanted settings.
func buildEditBody(old *dInspect, cur, want editSpec) (map[string]any, []extraNet) {
	body, extra := buildCreateBody(old, want.Image)
	host := body["HostConfig"].(map[string]any)

	env := make([]any, 0, len(want.Env))
	for _, e := range want.Env {
		env = append(env, e.K+"="+e.V)
	}
	body["Env"] = env

	if !reflect.DeepEqual(cmdArgs(cur.Cmd), cmdArgs(want.Cmd)) {
		if args := cmdArgs(want.Cmd); len(args) == 0 {
			delete(body, "Cmd")
		} else {
			l := make([]any, len(args))
			for i, a := range args {
				l[i] = a
			}
			body["Cmd"] = l
		}
	}

	bindings := map[string]any{}
	if oldPB, ok := old.HostConfig["PortBindings"].(map[string]any); ok {
		for k, v := range oldPB { // the bindings the panel does not show (random or range host ports) stay
			bl, _ := v.([]any)
			for _, b := range bl {
				bm, _ := b.(map[string]any)
				if hp, err := strconv.Atoi(jstr(bm["HostPort"])); err != nil || hp == 0 {
					bindings[k] = append(toStrings(bindings[k]), b)
				}
			}
		}
	}
	exposed := map[string]any{}
	if ex, ok := body["ExposedPorts"].(map[string]any); ok {
		for k, v := range ex {
			exposed[k] = v
		}
	}
	for _, p := range want.Ports {
		key := fmt.Sprintf("%d/%s", p.Cont, p.Proto)
		ip := p.IP
		if ip == "0.0.0.0" {
			ip = ""
		}
		bindings[key] = append(toStrings(bindings[key]), map[string]any{"HostIp": ip, "HostPort": strconv.Itoa(p.Host)})
		exposed[key] = map[string]any{}
	}
	host["PortBindings"] = bindings
	body["ExposedPorts"] = exposed

	oldMount := map[string]struct{ mode, prop string }{}
	for _, m := range old.Mounts {
		oldMount[m.Destination] = struct{ mode, prop string }{m.Mode, m.Propagation}
	}
	binds := make([]any, 0, len(want.Mounts))
	for _, m := range want.Mounts {
		var opts []string
		if o, ok := oldMount[m.Dst]; ok {
			for _, t := range strings.Split(o.mode, ",") {
				if t != "" && t != "ro" && t != "rw" {
					opts = append(opts, t)
				}
			}
			if m.Type == "bind" && o.prop != "" && o.prop != "rprivate" {
				opts = append(opts, o.prop)
			}
		}
		if m.RO {
			opts = append(opts, "ro")
		}
		s := m.Src + ":" + m.Dst
		if len(opts) > 0 {
			s += ":" + strings.Join(opts, ",")
		}
		binds = append(binds, s)
	}
	host["Binds"] = binds
	if ms, ok := host["Mounts"].([]any); ok { // the other kinds (tmpfs ...) stay
		var keep []any
		for _, m := range ms {
			if mm, _ := m.(map[string]any); mm["Type"] != "bind" && mm["Type"] != "volume" {
				keep = append(keep, m)
			}
		}
		if keep == nil {
			delete(host, "Mounts")
		} else {
			host["Mounts"] = keep
		}
	}

	host["RestartPolicy"] = map[string]any{"Name": want.Restart, "MaximumRetryCount": want.Retries}
	if cur.MemMB != want.MemMB {
		b := int64(want.MemMB) * miB
		host["Memory"] = b
		if b == 0 {
			host["MemorySwap"] = int64(0)
		} else {
			host["MemorySwap"] = memSwapFor(old, b)
		}
	}
	if math.Abs(cur.CPUs-want.CPUs) > 0.0005 {
		host["NanoCpus"] = int64(math.Round(want.CPUs * 1e9))
	}
	return body, extra
}

// samePort tells whether a published port of another container is the one the container wants.
func samePort(ip string, port int, proto string, p editPort) bool {
	return port == p.Host && proto == p.Proto && (wildcardIP(ip) || wildcardIP(p.IP) || ip == p.IP)
}

func checkPorts(ctx context.Context, selfID string, ports []editPort) error {
	var cs []dContainer
	if err := dockerJSON(ctx, http.MethodGet, "/containers/json?all=1", &cs); err != nil {
		return err
	}
	for _, c := range cs {
		if c.ID == selfID {
			continue
		}
		for _, cp := range c.Ports {
			if cp.PublicPort == 0 {
				continue
			}
			for _, p := range ports {
				if samePort(cp.IP, cp.PublicPort, cp.Type, p) {
					return fmt.Errorf("port %d/%s is already published by the container %s", p.Host, p.Proto, strings.TrimPrefix(first(c.Names), "/"))
				}
			}
		}
	}
	return nil
}

// dockerEdit is the stream "docker.edit": params {container, spec}.
func dockerEdit(ctx context.Context, raw json.RawMessage, sio *StreamIO) error {
	var p struct {
		Container string
		Spec      editSpec
	}
	if err := json.Unmarshal(raw, &p); err != nil || !containerRe.MatchString(p.Container) {
		return errors.New("invalid parameters")
	}
	want := normalizeSpec(p.Spec)
	if err := validateSpec(want); err != nil {
		return err
	}
	say := func(format string, args ...any) error { return sio.Out([]byte(fmt.Sprintf(format, args...) + "\n")) }
	var old dInspect
	if err := dockerJSON(ctx, http.MethodGet, "/containers/"+p.Container+"/json", &old); err != nil {
		return err
	}
	cur := currentSpec(&old, imageEnv(ctx, old.Image))
	plan, err := planEdit(cur, want, stringMap(old.Config["Labels"])[composeProject])
	if err != nil {
		return err
	}
	if len(plan.changed) == 0 {
		return say("Nothing to change.")
	}
	_ = say("Changes: %s", strings.Join(plan.changed, ", "))
	if plan.has("name") {
		if dockerJSON(ctx, http.MethodGet, "/containers/"+url.PathEscape(want.Name)+"/json", nil) == nil {
			return fmt.Errorf("the name %s is already used by another container", want.Name)
		}
	}
	if plan.recreate {
		return editRecreate(ctx, &old, cur, want, plan, say)
	}
	return editInPlace(ctx, &old, want, plan, say)
}

func editInPlace(ctx context.Context, old *dInspect, want editSpec, plan editPlan, say func(string, ...any) error) error {
	upd := map[string]any{}
	if plan.has("restart") {
		upd["RestartPolicy"] = map[string]any{"Name": want.Restart, "MaximumRetryCount": want.Retries}
	}
	if plan.has("memory") {
		b := int64(want.MemMB) * miB
		upd["Memory"], upd["MemorySwap"] = b, memSwapFor(old, b)
	}
	if plan.has("cpus") {
		upd["NanoCpus"] = int64(math.Round(want.CPUs * 1e9))
	}
	if len(upd) > 0 {
		_ = say("Updating the restart policy and the limits")
		if err := dockerSend(ctx, http.MethodPost, "/containers/"+old.ID+"/update", upd, nil); err != nil {
			return fmt.Errorf("update: %w", err)
		}
	}
	if plan.has("name") {
		_ = say("Renaming %s to %s", strings.TrimPrefix(old.Name, "/"), want.Name)
		if err := dockerSend(ctx, http.MethodPost, "/containers/"+old.ID+"/rename?name="+url.QueryEscape(want.Name), nil, nil); err != nil {
			return fmt.Errorf("rename: %w", err)
		}
	}
	return say("Done: the settings are applied, the container keeps running.")
}

func editRecreate(ctx context.Context, old *dInspect, cur, want editSpec, plan editPlan, say func(string, ...any) error) error {
	if plan.has("ports") {
		if err := checkPorts(ctx, old.ID, want.Ports); err != nil {
			return err
		}
	}
	if plan.has("image") {
		if imageIDRe.MatchString(want.Image) {
			return errors.New("enter an image name, not an id (for example nginx:latest)")
		}
		_ = say("Pulling %s", want.Image)
		if err := pullImage(ctx, want.Image, func(l string) error { return say("%s", l) }); err != nil {
			return err
		}
	}
	build := func() (map[string]any, []extraNet) { return buildEditBody(old, cur, want) }
	if err := swapContainer(ctx, old, want.Name, build, say); err != nil {
		return err
	}
	return say("Done: %s runs with the new settings. The old container is removed; its data in volumes stays.", want.Name)
}
