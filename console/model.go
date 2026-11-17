package console

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/bolgov0zero/linager/backup"
	"github.com/bolgov0zero/linager/installer"
)

const (
	refreshEvery  = 5 * time.Second
	updateEvery   = 30 * time.Minute
	pauseScript   = `"$@"; c=$?; printf '\n%s ' "$LINAGER_PAUSE"; read -r _; exit $c`
	maxBackupList = 9
)

type screen int

const (
	scrPick screen = iota
	scrMenu
	scrInstall
	scrConfirm
	scrChoose
	scrForm
	scrInfo
)

func (s screen) base() bool { return s == scrPick || s == scrMenu || s == scrInstall }

// run describes one command started from the menu.
type run struct {
	argv    []string // arguments of the linager binary, or a whole command when raw
	raw     bool
	sudo    bool
	pause   bool   // wait for Enter after it, so its output can be read
	audit   string // audit action to write afterwards (server only)
	cleanup func()
}

type entry struct {
	label string
	sudo  bool
	do    func() tea.Cmd
}

type confirmState struct {
	text string
	then func() tea.Cmd
}

type chooseState struct {
	title string
	items []entry
}

type infoState struct {
	title string
	lines []string
	bad   bool
}

type field struct {
	label    string
	secret   bool
	def      string
	optional bool
	repeat   int // index of the field this one must repeat, -1 for none
}

type formState struct {
	title  string
	fields []field
	inputs []textinput.Model
	idx    int
	err    string
	done   func(values []string) (run, error)
}

// Messages.
type (
	tickMsg   struct{}
	statusMsg struct {
		roles  []Role
		srv    *ServerStatus
		agt    *AgentStatus
		manual bool
	}
	latestMsg   struct{ tag string }
	execDoneMsg struct {
		r   run
		err error
	}
)

type model struct {
	lang    installer.Lang
	version string
	self    string
	probe   *Probe
	root    bool

	roles  []Role
	role   Role
	home   screen // the base screen under any dialog
	scr    screen
	cursor int
	w      int

	srv        *ServerStatus
	agt        *AgentStatus
	latest     string
	lastUpdate time.Time

	cf   *confirmState
	ch   *chooseState
	form *formState
	info *infoState
}

func newModel(o Options, probe *Probe) *model {
	self, err := os.Executable()
	if err != nil {
		self = "linager"
	}
	m := &model{lang: o.Lang, version: o.Version, self: self, probe: probe, root: os.Geteuid() == 0, w: 80}
	m.roles = probe.Layout.Detect()
	m.reconcile()
	m.scr = m.home
	return m
}

func (m *model) t(key string, args ...any) string { return tr(m.lang, key, args...) }

// reconcile picks the base screen from the roles found; it runs again after every refresh, so the menu follows
// an installation or a removal made from the menu itself.
func (m *model) reconcile() {
	prev, prevRole := m.home, m.role
	switch len(m.roles) {
	case 0:
		m.home = scrInstall
	case 1:
		m.home, m.role = scrMenu, m.roles[0]
	default:
		if m.home != scrMenu || !slices.Contains(m.roles, m.role) {
			m.home = scrPick
		}
	}
	if m.home != prev || m.role != prevRole {
		m.cursor = 0
		if m.scr.base() {
			m.scr = m.home
		}
	}
}

// ---- commands ----

func (m *model) collect(manual bool) tea.Cmd {
	probe := m.probe
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		msg := statusMsg{roles: probe.Layout.Detect(), manual: manual}
		if slices.Contains(msg.roles, RoleServer) {
			msg.srv = probe.Server(ctx)
		}
		if slices.Contains(msg.roles, RoleAgent) {
			msg.agt = probe.Agent(ctx)
		}
		return msg
	}
}

func (m *model) checkLatest() tea.Cmd {
	probe := m.probe
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return latestMsg{tag: probe.Latest(ctx)}
	}
}

func (m *model) auditCmd(action string, ok bool) tea.Cmd {
	probe := m.probe
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		probe.Audit(ctx, action, ok)
		return nil
	}
}

func tick() tea.Cmd { return tea.Tick(refreshEvery, func(time.Time) tea.Msg { return tickMsg{} }) }

// command builds the process of a run: the linager binary (or a raw command), through sudo when this user is not
// root, and wrapped so the output stays on the screen until Enter.
func (m *model) command(r run) *exec.Cmd {
	argv := r.argv
	if !r.raw {
		argv = append([]string{m.self}, argv...)
	}
	if r.sudo && !m.root {
		argv = append([]string{"sudo"}, argv...)
	}
	var c *exec.Cmd
	if r.pause {
		c = exec.Command("/bin/sh", append([]string{"-c", pauseScript, "sh"}, argv...)...) //nolint:gosec // fixed script, arguments are passed separately
		c.Env = append(os.Environ(), "LINAGER_PAUSE="+m.t("pause"))
	} else {
		c = exec.Command(argv[0], argv[1:]...) //nolint:gosec // the arguments are built by the menu
	}
	return c
}

func (m *model) start(r run) tea.Cmd {
	if r.sudo && !m.root {
		if _, err := exec.LookPath("sudo"); err != nil {
			m.showInfo(m.t("err.sudo"), true)
			if r.cleanup != nil {
				r.cleanup()
			}
			return nil
		}
	}
	return tea.ExecProcess(m.command(r), func(err error) tea.Msg { return execDoneMsg{r: r, err: err} })
}

func (m *model) showInfo(text string, bad bool) {
	m.info = &infoState{lines: strings.Split(text, "\n"), bad: bad}
	m.scr = scrInfo
}

func (m *model) askConfirm(text string, then func() tea.Cmd) tea.Cmd {
	m.cf = &confirmState{text: text, then: then}
	m.scr = scrConfirm
	return nil
}

func (m *model) goHome() { m.scr = m.home; m.cursor = 0 }

// ---- menus ----

func (m *model) systemctl(verb, unit string) run {
	return run{raw: true, argv: []string{"systemctl", verb, unit}, sudo: true, pause: true}
}

func (m *model) logs(unit string) run {
	return run{raw: true, argv: []string{"journalctl", "-u", unit, "-n", "200", "-f", "--no-pager"}, sudo: true}
}

func (m *model) entries() []entry {
	t := m.t
	runIt := func(r run) func() tea.Cmd { return func() tea.Cmd { return m.start(r) } }
	var items []entry
	var last entry
	switch m.scr {
	case scrPick:
		items = []entry{
			{label: t("role.server"), do: func() tea.Cmd { return m.enter(RoleServer) }},
			{label: t("role.agent"), do: func() tea.Cmd { return m.enter(RoleAgent) }},
		}
		last = entry{label: t("exit"), do: func() tea.Cmd { return tea.Quit }}
	case scrInstall:
		items = []entry{
			{label: t("m.i.server"), sudo: true, do: runIt(run{argv: []string{"install", "server"}, sudo: true, pause: true})},
			{label: t("m.i.agent"), sudo: true, do: runIt(run{argv: []string{"install", "agent"}, sudo: true, pause: true})},
		}
		last = entry{label: t("exit"), do: func() tea.Cmd { return tea.Quit }}
	case scrChoose:
		items = m.ch.items
		last = entry{label: t("back"), do: func() tea.Cmd { m.scr = m.home; return nil }}
	case scrMenu:
		if m.role == RoleServer {
			items = m.serverEntries(runIt)
		} else {
			items = m.agentEntries(runIt)
		}
		if len(m.roles) > 1 {
			last = entry{label: t("back"), do: func() tea.Cmd { m.home, m.scr, m.cursor = scrPick, scrPick, 0; return nil }}
		} else {
			last = entry{label: t("exit"), do: func() tea.Cmd { return tea.Quit }}
		}
	}
	return append(items, last)
}

func (m *model) enter(r Role) tea.Cmd {
	m.role, m.home, m.scr, m.cursor = r, scrMenu, scrMenu, 0
	return nil
}

func (m *model) serverEntries(runIt func(run) func() tea.Cmd) []entry {
	t := m.t
	return []entry{
		{label: t("m.s.update"), sudo: true, do: runIt(run{argv: []string{"update"}, sudo: true, pause: true, audit: "console.update"})},
		{label: t("m.s.backup"), sudo: true, do: runIt(run{argv: []string{"backup"}, sudo: true, pause: true, audit: "console.backup"})},
		{label: t("m.s.restore"), sudo: true, do: m.openRestore},
		{label: t("m.s.password"), sudo: true, do: m.openPassword},
		{label: t("m.s.access"), sudo: true, do: func() tea.Cmd {
			return m.askConfirm(t("c.access"), runIt(run{argv: []string{"access", "reset"}, sudo: true, pause: true}))
		}},
		{label: t("m.s.fp"), do: m.openFingerprint},
		{label: t("m.s.restart"), sudo: true, do: func() tea.Cmd {
			r := m.systemctl("restart", "linager-server")
			r.audit = "console.service_restart"
			return m.askConfirm(t("c.restart", "linager-server"), runIt(r))
		}},
		{label: t("m.s.logs"), sudo: true, do: runIt(m.logs("linager-server"))},
	}
}

func (m *model) agentEntries(runIt func(run) func() tea.Cmd) []entry {
	t := m.t
	return []entry{
		{label: t("m.a.enroll"), sudo: true, do: func() tea.Cmd {
			if m.agt != nil && m.agt.Enrolled {
				return m.askConfirm(t("c.reenroll"), m.openEnroll)
			}
			return m.openEnroll()
		}},
		{label: t("m.a.update"), sudo: true, do: runIt(run{argv: []string{"agent", "update"}, sudo: true, pause: true})},
		{label: t("m.a.restart"), sudo: true, do: func() tea.Cmd {
			return m.askConfirm(t("c.restart", "linager-agent"), runIt(m.systemctl("restart", "linager-agent")))
		}},
		{label: t("m.a.logs"), sudo: true, do: runIt(m.logs("linager-agent"))},
		{label: t("m.a.uninstall"), sudo: true, do: func() tea.Cmd {
			m.ch = &chooseState{title: t("t.uninstall"), items: []entry{
				{label: t("o.keep"), do: runIt(run{argv: []string{"agent", "uninstall"}, sudo: true, pause: true})},
				{label: t("o.purge"), do: runIt(run{argv: []string{"agent", "uninstall", "--purge"}, sudo: true, pause: true})},
			}}
			m.scr, m.cursor = scrChoose, 0
			return nil
		}},
	}
}

func (m *model) openRestore() tea.Cmd {
	list := backup.List(backup.DefaultDir, "linager-backup")
	if len(list) == 0 {
		m.showInfo(m.t("t.nobackup", backup.DefaultDir), true)
		return nil
	}
	var items []entry
	for i, fi := range list {
		if i == maxBackupList {
			break
		}
		path := backup.DefaultDir + "/" + fi.Name()
		items = append(items, entry{
			label: fmt.Sprintf("%s  %s  %s", fi.Name(), fi.ModTime().Format("2006-01-02 15:04"), fmtBytes(fi.Size())),
			do: func() tea.Cmd {
				r := run{argv: []string{"restore", path, "--yes"}, sudo: true, pause: true, audit: "console.restore"}
				m.scr = m.home
				return m.askConfirm(m.t("c.restore", path), func() tea.Cmd { return m.start(r) })
			},
		})
	}
	m.ch = &chooseState{title: m.t("t.restore"), items: items}
	m.scr, m.cursor = scrChoose, 0
	return nil
}

func (m *model) openFingerprint() tea.Cmd {
	fp, listen, err := m.probe.Fingerprint()
	if err != nil {
		m.showInfo(m.t("err.root"), true)
		return nil
	}
	m.info = &infoState{title: m.t("i.fp.title"), lines: []string{
		m.t("i.fp.fp") + ": " + fp,
		m.t("i.fp.port") + ": " + listen,
		"",
		m.t("i.fp.cmd"),
		"  linager install agent --server " + listen + " --fingerprint " + fp,
	}}
	m.scr = scrInfo
	return nil
}

func (m *model) openForm(f *formState) tea.Cmd {
	for _, fd := range f.fields {
		ti := textinput.New()
		ti.Prompt = "› "
		ti.Width = 40
		ti.SetValue(fd.def)
		if fd.secret {
			ti.EchoMode = textinput.EchoPassword
		}
		f.inputs = append(f.inputs, ti)
	}
	f.inputs[0].Focus()
	m.form, m.scr = f, scrForm
	return nil
}

func (m *model) openPassword() tea.Cmd {
	create := m.srv != nil && m.srv.Users == 0
	return m.openForm(&formState{
		title: m.t("f.pw.title"),
		fields: []field{
			{label: m.t("f.login"), def: "admin", repeat: -1},
			{label: m.t("f.password"), secret: true, repeat: -1},
			{label: m.t("f.repeat"), secret: true, repeat: 1},
		},
		done: func(v []string) (run, error) {
			f, err := os.CreateTemp("", "linager-pw-")
			if err != nil {
				return run{}, err
			}
			_, werr := f.WriteString(v[1])
			if cerr := f.Close(); werr == nil {
				werr = cerr
			}
			if werr != nil {
				_ = os.Remove(f.Name())
				return run{}, werr
			}
			sub, audit := "reset-password", ""
			if create {
				sub, audit = "create-admin", "console.create_admin"
			}
			return run{argv: []string{"server", sub, "--login", v[0], "--password-file", f.Name()}, sudo: true, pause: true, audit: audit,
				cleanup: func() { _ = os.Remove(f.Name()) }}, nil
		},
	})
}

func (m *model) openEnroll() tea.Cmd {
	return m.openForm(&formState{
		title: m.t("f.enroll.title"),
		fields: []field{
			{label: m.t("f.server"), repeat: -1},
			{label: m.t("f.fp"), optional: true, repeat: -1},
		},
		done: func(v []string) (run, error) {
			argv := []string{"agent", "enroll", "--server", v[0]}
			if v[1] != "" {
				argv = append(argv, "--fingerprint", v[1])
			}
			return run{argv: argv, sudo: true, pause: true}, nil
		},
	})
}

// ---- bubbletea ----

// Init implements tea.Model.
func (m *model) Init() tea.Cmd { return m.collect(false) }

// Update implements tea.Model.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w = msg.Width
	case tickMsg:
		return m, m.collect(false)
	case statusMsg:
		m.roles, m.srv, m.agt = msg.roles, msg.srv, msg.agt
		m.reconcile()
		cmds := []tea.Cmd{}
		if !msg.manual {
			cmds = append(cmds, tick())
		}
		if m.srv != nil && time.Since(m.lastUpdate) > updateEvery {
			m.lastUpdate = time.Now()
			cmds = append(cmds, m.checkLatest())
		}
		return m, tea.Batch(cmds...)
	case latestMsg:
		m.latest = msg.tag
	case execDoneMsg:
		if msg.r.cleanup != nil {
			msg.r.cleanup()
		}
		cmds := []tea.Cmd{m.collect(true)}
		if msg.r.audit != "" {
			cmds = append(cmds, m.auditCmd(msg.r.audit, msg.err == nil))
		}
		return m, tea.Batch(cmds...)
	case tea.KeyMsg:
		return m, m.key(msg)
	}
	return m, nil
}

func (m *model) key(msg tea.KeyMsg) tea.Cmd {
	k := msg.String()
	if k == "ctrl+c" {
		return tea.Quit
	}
	switch m.scr {
	case scrConfirm:
		then := m.cf.then
		m.scr = m.home
		if k == "y" || k == "Y" || k == "д" || k == "Д" {
			return then()
		}
		return nil
	case scrInfo:
		m.scr = m.home
		return nil
	case scrForm:
		return m.formKey(msg)
	}
	items := m.entries()
	n := len(items)
	switch k {
	case "up", "k", "left":
		m.cursor = (m.cursor + n - 1) % n
	case "down", "j", "right", "tab":
		m.cursor = (m.cursor + 1) % n
	case "enter", " ":
		return items[m.cursor].do()
	case "esc", "0":
		return items[n-1].do()
	case "q", "й":
		if m.scr.base() {
			return tea.Quit
		}
		return items[n-1].do()
	default:
		if len(k) == 1 && k[0] >= '1' && k[0] <= '9' {
			if i := int(k[0] - '1'); i < n-1 {
				m.cursor = i
				return items[i].do()
			}
		}
	}
	return nil
}

func (m *model) formKey(msg tea.KeyMsg) tea.Cmd {
	f := m.form
	switch msg.String() {
	case "esc":
		m.goHome()
		return nil
	case "enter":
		val := f.inputs[f.idx].Value()
		fd := f.fields[f.idx]
		switch {
		case val == "" && !fd.optional:
			f.err = m.t("f.required")
			return nil
		case fd.repeat >= 0 && val != f.inputs[fd.repeat].Value():
			f.err = m.t("f.mismatch")
			f.inputs[f.idx].SetValue("")
			f.inputs[fd.repeat].SetValue("")
			f.inputs[f.idx].Blur()
			f.idx = fd.repeat
			f.inputs[f.idx].Focus()
			return nil
		}
		f.err = ""
		if f.idx < len(f.fields)-1 {
			f.inputs[f.idx].Blur()
			f.idx++
			f.inputs[f.idx].Focus()
			return nil
		}
		vals := make([]string, len(f.inputs))
		for i := range f.inputs {
			vals[i] = f.inputs[i].Value()
		}
		r, err := f.done(vals)
		m.goHome()
		if err != nil {
			m.showInfo(m.t("err.exec", err.Error()), true)
			return nil
		}
		return m.start(r)
	}
	var cmd tea.Cmd
	f.inputs[f.idx], cmd = f.inputs[f.idx].Update(msg)
	return cmd
}

// ---- view ----

func (m *model) width() int { return max(40, min(m.w, 88)) }

func (m *model) modeName() string {
	switch {
	case len(m.roles) == 0:
		return m.t("role.none")
	case m.home == scrMenu:
		return m.t("role." + string(m.role))
	}
	return m.t("role.server") + " + " + m.t("role.agent")
}

// View implements tea.Model.
func (m *model) View() string {
	var b strings.Builder
	b.WriteString("\n " + strings.ReplaceAll(header(m.lang, m.version, m.modeName(), m.latest), "\n", "\n ") + "\n\n")
	body, foot := m.body()
	b.WriteString(indent(body, " ") + "\n\n " + hint(foot) + "\n")
	if m.needRoot() {
		b.WriteString(" " + style(cYellow).Render(m.t("needroot")) + "\n")
	}
	return b.String()
}

func indent(s, p string) string { return p + strings.ReplaceAll(s, "\n", "\n"+p) }

func (m *model) needRoot() bool {
	return !m.root && ((m.srv != nil && m.srv.NeedRoot) || (m.agt != nil && m.agt.NeedRoot))
}

func (m *model) list(items []entry) string {
	var lines []string
	lw := 0
	for _, e := range items {
		lw = max(lw, lipgloss.Width(e.label))
	}
	for i, e := range items {
		num, tag := fmt.Sprint(i+1), ""
		if i == len(items)-1 {
			num = "0"
		}
		if e.sudo && !m.root {
			tag = m.t("sudo")
		}
		lines = append(lines, entryLine(num, e.label+strings.Repeat(" ", lw-lipgloss.Width(e.label)), tag, i == m.cursor))
	}
	return strings.Join(lines, "\n")
}

func (m *model) body() (string, string) {
	switch m.scr {
	case scrPick:
		items := m.entries()
		w := m.width() - 2
		cw, join := w, lipgloss.JoinVertical
		if w >= 70 {
			cw = (w - 1) / 2
			join = func(_ lipgloss.Position, s ...string) string {
				return lipgloss.JoinHorizontal(lipgloss.Top, s[0], " ", s[1])
			}
		}
		cards := join(lipgloss.Left,
			card("1  "+strings.ToUpper(m.t("role.server")), serverRows(m.lang, m.srv, m.version, m.latest, true), cw, m.cursor == 0),
			card("2  "+strings.ToUpper(m.t("role.agent")), agentRows(m.lang, m.agt, m.version, true), cw, m.cursor == 1))
		// the cards are the entries 1 and 2; only the exit line is listed
		exit := entryLine("0", items[len(items)-1].label, "", m.cursor == len(items)-1)
		return cards + "\n\n" + exit, m.t("hint.menu")
	case scrInstall:
		return style(cText).Render(m.t("i.none")) + "\n" + hint(m.t("i.hint")) + "\n\n" + m.list(m.entries()), m.t("hint.menu")
	case scrMenu:
		var rows []kv
		if m.role == RoleServer {
			rows = serverRows(m.lang, m.srv, m.version, m.latest, false)
		} else {
			rows = agentRows(m.lang, m.agt, m.version, false)
		}
		return table(rows) + "\n\n" + m.list(m.entries()), m.t("hint.menu")
	case scrChoose:
		return style(cAccent).Bold(true).Render(m.ch.title) + "\n\n" + m.list(m.entries()), m.t("hint.sub")
	case scrConfirm:
		return style(cYellow).Render(wrap(m.cf.text, m.width()-2)), m.t("hint.confirm")
	case scrInfo:
		c := cText
		if m.info.bad {
			c = cRed
		}
		out := ""
		if m.info.title != "" {
			out = style(cAccent).Bold(true).Render(m.info.title) + "\n\n"
		}
		return out + style(c).Render(strings.Join(m.info.lines, "\n")), m.t("hint.any")
	case scrForm:
		return m.formView(), m.t("hint.form")
	}
	return "", ""
}

func (m *model) formView() string {
	f := m.form
	var lines []string
	lines = append(lines, style(cAccent).Bold(true).Render(f.title), "")
	for i := range f.fields {
		switch {
		case i < f.idx:
			v := f.inputs[i].Value()
			if f.fields[i].secret {
				v = strings.Repeat("•", len(v))
			}
			lines = append(lines, style(cGrey).Render(f.fields[i].label+": ")+v)
		case i == f.idx:
			lines = append(lines, style(cText).Render(f.fields[i].label), f.inputs[i].View())
		}
	}
	if f.err != "" {
		lines = append(lines, "", style(cRed).Render(f.err))
	}
	return strings.Join(lines, "\n")
}

// wrap breaks text into lines of at most w cells at spaces.
func wrap(s string, w int) string {
	var out, line []string
	n := 0
	for _, word := range strings.Fields(s) {
		ww := lipgloss.Width(word)
		if n > 0 && n+1+ww > w {
			out = append(out, strings.Join(line, " "))
			line, n = nil, 0
		}
		if n > 0 {
			n++
		}
		line = append(line, word)
		n += ww
	}
	if len(line) > 0 {
		out = append(out, strings.Join(line, " "))
	}
	return strings.Join(out, "\n")
}
