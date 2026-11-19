// Package tui is the CaddyMan terminal interface (Bubble Tea, Emacs keys).
package tui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"caddyman/internal/caddy"
	"caddyman/internal/config"
	"caddyman/internal/serviceinfo"
	"caddyman/internal/services"
)

type mode int

const (
	modeList mode = iota
	modePager
	modeForm
)

type Model struct {
	cfg  *config.Config
	w, h int
	mode mode

	cursor, offset int
	filter         string

	// caddy is Caddy's last known state (statusKnown once a check finished);
	// caddyVersion is its version, read once it's found.
	caddy        caddy.Status
	statusKnown  bool
	caddyVersion string
	// units are the states of the sites' services, by "<scope>/<unit>";
	// tracked are ServiceMan's services, offered by the site form.
	units   map[string]services.State
	tracked []services.Unit
	// info is what the tracked services' binaries said with caddyman-info,
	// by key (serviceInfosCmd); infoFor is the tracked list it was asked for.
	// infoCache keeps binaries from being run again until they or their
	// config change (nil: no caching).
	info      map[string]*serviceinfo.Info
	infoFor   string
	infoCache *serviceinfo.Cache
	// refreshSeq numbers status refreshes; statusSeq is the newest one shown.
	refreshSeq, statusSeq int

	// pending is what Apply changes would change in Caddy's directory.
	pending caddy.PendingSet

	// busy is the change being saved or applied ("" when none): one at a
	// time.
	busy string

	msg    string
	msgErr bool
	msgSeq int // bumped by setMsg/setErr so each message gets its own 5 s timer
	prefix string

	// showPreview shows the site file's preview in the Add/Edit Site form. It's
	// hidden until C-c C-p turns it on, and stays that way for the session.
	showPreview bool

	prompt *prompt
	pager  *pager
	form   *form
}

func Run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	m := &Model{cfg: cfg, infoCache: &serviceinfo.Cache{}}
	if len(cfg.Sites) == 0 {
		m.setMsg("No sites yet — press c to add one, ? for help")
	}
	if v, err := config.FileVersion(config.Path()); err == nil && v < config.CurrentVersion {
		m.setErr("Your config is version %d, current is %d — run `caddyman config update` to add the new settings", v, config.CurrentVersion)
	}
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

// ------------------------------------------------------------------ messages

type tickMsg struct{}

type statusMsg struct {
	seq     int
	status  caddy.Status
	version string         // "" when not asked or unknown
	cfg     *config.Config // the config on disk (nil when it couldn't be read)
	units   map[string]services.State
	tracked []services.Unit
	pending caddy.PendingSet
}

// savedMsg is the end of a change to the sites (caddy.Update): saved in
// CaddyMan's directory, not applied yet.
type savedMsg struct {
	desc  string
	cfg   *config.Config
	saved caddy.Saved
	err   error
	form  *form // the form the change came from, if any
	after func(m *Model)
}

// appliedMsg is the end of Apply changes. out is the script's output when
// it ran without the terminal.
type appliedMsg struct {
	n   int // sites changed
	out string
	err error
}

// missingMsg lists enabled sites Caddy's config doesn't have after an
// apply.
type missingMsg []string

// execMsg hands the terminal to a command (sudo asking for your password).
type execMsg struct {
	cmd   *exec.Cmd
	after func(err error) tea.Msg
}

const refreshEvery = 5 * time.Second

func tick() tea.Cmd {
	return tea.Tick(refreshEvery, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *Model) Init() tea.Cmd {
	s := m.msgSeq // the startup message expires too
	clear := tea.Tick(msgTimeout, func(time.Time) tea.Msg { return clearMsgMsg{s} })
	return tea.Batch(m.refreshCmd(), tick(), clear)
}

// refreshCmd checks Caddy and the sites' services, and re-reads the config
// (changed by `caddyman apply` or by hand) and ServiceMan's services, off
// the UI goroutine.
func (m *Model) refreshCmd() tea.Cmd {
	m.refreshSeq++
	seq := m.refreshSeq
	bin, haveVer := m.cfg.CaddyBinPath(), m.caddyVersion != ""
	smPath := m.cfg.ServicemanConfigPath()
	return func() tea.Msg {
		msg := statusMsg{seq: seq, status: caddy.GetStatus(bin)}
		if p, err := caddy.Find(bin); err == nil && !haveVer {
			msg.version, _ = caddy.Version(p)
		}
		c, err := config.Load()
		if err == nil {
			msg.cfg = c
		}
		msg.tracked, _ = services.Tracked(smPath)
		if c != nil {
			var units []services.Unit
			for _, s := range c.Sites {
				if s.Service != "" {
					units = append(units, services.Unit{Name: s.Service, Scope: s.ServiceScope})
				}
			}
			msg.units = services.States(units)
			msg.pending, _ = caddy.Pending(c)
		}
		return msg
	}
}

// ------------------------------------------------------------------ helpers

func (m *Model) setMsg(format string, a ...any) {
	m.msg, m.msgErr = fmt.Sprintf(format, a...), false
	m.msgSeq++
}
func (m *Model) setErr(format string, a ...any) {
	m.msg, m.msgErr = fmt.Sprintf(format, a...), true
	m.msgSeq++
}

// msgTimeout is how long an echo-area message stays before the key
// reference comes back. Errors don't expire; the next key clears them.
const msgTimeout = 5 * time.Second

type clearMsgMsg struct{ seq int }

// rows returns the sites matching the current filter, ordered by address.
func (m *Model) rows() []config.Site {
	all := m.cfg.Sorted()
	if m.filter == "" {
		return all
	}
	var out []config.Site
	for _, s := range all {
		if matchAll(m.filter, m.cfg.Address(s)+" "+s.Target()) {
			out = append(out, s)
		}
	}
	return out
}

func matchAll(query, hay string) bool {
	hay = strings.ToLower(hay)
	for _, t := range strings.Fields(strings.ToLower(query)) {
		if !strings.Contains(hay, t) {
			return false
		}
	}
	return true
}

func (m *Model) selected() *config.Site {
	rows := m.rows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return nil
	}
	s := rows[m.cursor]
	return &s
}

// selectAddr puts the cursor on the site served at addr.
func (m *Model) selectAddr(addr string) {
	for i, s := range m.rows() {
		if m.cfg.Address(s) == addr {
			m.cursor = i
			return
		}
	}
	m.clampCursor()
}

func (m *Model) clampCursor() {
	n := len(m.rows())
	if m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// saveChange changes the config with fn and saves it in CaddyMan's
// directory (site files checked with Caddy, then the config) off the UI
// goroutine. after runs when it worked, before the result message is set.
// Nothing reaches Caddy until Apply changes.
func (m *Model) saveChange(desc string, fn func(*config.Config) error, from *form, after func(m *Model)) tea.Cmd {
	if m.busy != "" {
		m.setErr("Still busy: %s", m.busy)
		return nil
	}
	m.busy = desc
	m.setMsg("%s…", desc)
	return func() tea.Msg {
		c, s, err := caddy.Update(fn)
		return savedMsg{desc: desc, cfg: c, saved: s, err: err, form: from, after: after}
	}
}

// applyChanges is Apply changes: regenerate and check CaddyMan's site
// files, then copy them into Caddy's directory as root and reload Caddy.
// sudo asks for your password on the terminal when it needs one. always
// applies even with nothing to change (Reload Caddy).
func (m *Model) applyChanges(always bool) tea.Cmd {
	if m.busy != "" {
		m.setErr("Still busy: %s", m.busy)
		return nil
	}
	n := m.pending.Count()
	if n == 0 && !always {
		m.setMsg("No changes to apply")
		return nil
	}
	m.busy = "Applying changes"
	m.setMsg("Applying changes…")
	return func() tea.Msg {
		c, err := config.Load()
		if err == nil {
			_, err = caddy.SaveLocal(c, nil)
		}
		if err != nil {
			return appliedMsg{n: n, err: err}
		}
		out, needAuth, err := caddy.DeployQuick(c)
		if !needAuth {
			return appliedMsg{n: n, out: out, err: err}
		}
		cmd, err := caddy.DeployCmd(c, true)
		if err != nil {
			return appliedMsg{n: n, err: err}
		}
		return execMsg{cmd, func(err error) tea.Msg { return appliedMsg{n: n, err: err} }}
	}
}

// lastLine is the last non-empty line of out.
func lastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// ------------------------------------------------------------------ update

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if c, ok := msg.(clearMsgMsg); ok {
		// Only the latest message expires; errors and prefix prompts like
		// "C-x-" stay until the next key.
		if c.seq == m.msgSeq && m.prefix == "" && !m.msgErr {
			m.msg, m.msgErr = "", false
		}
		return m, nil
	}
	seq := m.msgSeq
	cmd := m.update(msg)
	if m.msgSeq != seq && m.msg != "" && !m.msgErr {
		s := m.msgSeq
		cmd = tea.Batch(cmd, tea.Tick(msgTimeout, func(time.Time) tea.Msg { return clearMsgMsg{s} }))
	}
	return m, cmd
}

func (m *Model) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return nil

	case tickMsg:
		if m.refreshSeq != m.statusSeq {
			return tick() // the last refresh is still running: don't stack another
		}
		return tea.Batch(m.refreshCmd(), tick())

	case statusMsg:
		if msg.seq < m.statusSeq {
			return nil // an older refresh that finished after a newer one
		}
		m.statusSeq = msg.seq
		m.caddy, m.statusKnown = msg.status, true
		if !msg.status.Found {
			m.caddyVersion = ""
		} else if msg.version != "" {
			m.caddyVersion = msg.version
		}
		m.units, m.tracked, m.pending = msg.units, msg.tracked, msg.pending
		// Pick up changes made outside the TUI, unless a change of ours is
		// in flight (its result is newer).
		if msg.cfg != nil && m.busy == "" {
			var addr string
			if s := m.selected(); s != nil {
				addr = m.cfg.Address(*s)
			}
			m.cfg = msg.cfg
			m.selectAddr(addr)
		}
		if trackedKeys(m.tracked) != m.infoFor {
			return m.serviceInfosCmd()
		}
		return nil

	case serviceInfoMsg:
		m.applyServiceInfo(msg)
		return nil
	case serviceInfosMsg:
		m.info = msg
		if f := m.form; f != nil {
			f.get("service").labelServices(m.tracked, m.info)
			if m.info[f.val("service")] != nil {
				f.setAuto(true)
			}
		}
		return nil
	case savedMsg:
		m.busy = ""
		if msg.err != nil {
			if msg.form != nil && m.form == msg.form {
				msg.form.err = msg.err.Error()
			}
			m.setErr("%s: %v", msg.desc, msg.err)
			return nil
		}
		var addr string
		if s := m.selected(); s != nil {
			addr = m.cfg.Address(*s)
		}
		m.cfg = msg.cfg
		m.selectAddr(addr)
		// A refresh still running read the config before this change: drop it.
		m.statusSeq = m.refreshSeq
		if msg.after != nil {
			msg.after(m)
		}
		m.setMsg("%s: saved — %s", msg.desc, msg.saved.Note())
		return m.refreshCmd()

	case execMsg:
		return tea.ExecProcess(msg.cmd, msg.after)

	case appliedMsg:
		m.busy = ""
		switch {
		case msg.err != nil && msg.out != "":
			m.setErr("Apply changes failed: %s", lastLine(msg.out))
		case msg.err != nil:
			m.setErr("Apply changes failed: %v", msg.err)
		case msg.n == 1:
			m.setMsg("Applied 1 change")
		default:
			m.setMsg("Applied %d changes", msg.n)
		}
		if msg.err != nil {
			return m.refreshCmd()
		}
		c := m.cfg
		return tea.Batch(m.refreshCmd(), func() tea.Msg { return missingMsg(caddy.Missing(c)) })

	case missingMsg:
		if len(msg) > 0 {
			m.setErr("Applied, but Caddy's config doesn't have %s: your Caddyfile doesn't import them (M-x Check Caddy setup)",
				strings.Join(msg, ", "))
		}
		return nil

	case staticLoadedMsg:
		if m.pager == msg.p { // the viewer wasn't closed meanwhile
			msg.p.loading = false
			msg.p.setLines(msg.lines)
		}
		return nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return nil
}

// quit asks first when there are changes Caddy doesn't have yet.
func (m *Model) quit() tea.Cmd {
	n := m.pending.Count()
	if n == 0 {
		return tea.Quit
	}
	q := fmt.Sprintf("%d changes aren't applied to Caddy; quit anyway?", n)
	if n == 1 {
		q = "1 change isn't applied to Caddy; quit anyway?"
	}
	m.yesOrNo(q, func(m *Model) tea.Cmd { return tea.Quit })
	return nil
}

// ------------------------------------------------------------------ view

func (m *Model) View() string {
	if m.w == 0 {
		return ""
	}
	footer := m.footer()
	bodyH := m.h - strings.Count(footer, "\n") - 1
	var body string
	switch {
	case m.paletteOpen():
		body = m.paletteView(m.w, bodyH)
	case m.mode == modePager:
		body = m.pager.view(m.w, bodyH)
	case m.mode == modeForm:
		body = m.form.view(m, m.w, bodyH)
	default:
		body = m.listView(m.w, bodyH)
	}
	// Force the body to exactly bodyH lines so the footer stays put.
	lines := strings.Split(body, "\n")
	if len(lines) > bodyH {
		lines = lines[:bodyH]
	}
	for len(lines) < bodyH {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n") + "\n" + footer
}
