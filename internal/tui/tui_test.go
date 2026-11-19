package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"caddyman/internal/caddy"
	"caddyman/internal/config"
	"caddyman/internal/services"
)

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

var (
	keyCN = tea.KeyMsg{Type: tea.KeyCtrlN}
	keyCP = tea.KeyMsg{Type: tea.KeyCtrlP}
	keyCG = tea.KeyMsg{Type: tea.KeyCtrlG}
	keyCC = tea.KeyMsg{Type: tea.KeyCtrlC}
)

func testModel(t *testing.T, sites ...config.Site) *Model {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_DATA_HOME", dir)
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	c.CaddyBin = filepath.Join(dir, "no-caddy") // not installed: nothing runs Caddy
	c.Caddyfile = filepath.Join(dir, "etc", "Caddyfile")
	c.CaddySitesDir = filepath.Join(dir, "etc", "caddyman")
	c.Sites = sites
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	return &Model{cfg: c, w: 110, h: 30, statusKnown: true, caddy: caddy.Status{Found: true, Running: true}, caddyVersion: "2.10.2",
		tracked: []services.Unit{
			{Name: "jellyfin.service", Scope: "user", URL: "http://localhost:8096/health"},
			{Name: "nginx.service", Scope: "system"},
		},
		units: map[string]services.State{"user/jellyfin.service": {Load: "loaded", Active: "active", Sub: "running"}},
	}
}

func view(m *Model) (string, []string) {
	v := ansi.Strip(m.View())
	return v, strings.Split(v, "\n")
}

// run runs a command and feeds its message (one level) back, like Bubble
// Tea would.
func run(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		for _, c := range b {
			if c != nil {
				if mm, ok := c().(savedMsg); ok {
					m.update(mm)
				}
			}
		}
		return
	}
	m.update(msg)
}

func TestListView(t *testing.T) {
	m := testModel(t,
		config.Site{Name: "b", Enabled: true, Kind: config.Proxy, Upstream: "localhost:8096", HTTPS: true, TrustedOnly: true, RedirectRoot: "/web/",
			Service: "jellyfin.service", ServiceScope: "user"},
		config.Site{Name: "a", Enabled: false, Kind: config.Proxy, Upstream: "localhost:3000"},
	)
	v, lines := view(m)
	if len(lines) != m.h {
		t.Errorf("view is %d lines, want %d", len(lines), m.h)
	}
	if !strings.Contains(lines[0], "CADDYMAN") || !strings.Contains(lines[0], "Sites") || !strings.Contains(lines[0], "Caddy: ● running v2.10.2") {
		t.Errorf("top bar = %q", lines[0])
	}
	// Sorted by address; the cursor starts on the first.
	if !strings.HasPrefix(lines[3], "▸ a.home.arpa") || !strings.Contains(lines[3], "○ off") {
		t.Errorf("first row = %q", lines[3])
	}
	if !strings.Contains(lines[4], "b.home.arpa") || !strings.Contains(lines[4], "● on") || !strings.Contains(lines[4], "● active jellyfin") {
		t.Errorf("second row = %q", lines[4])
	}
	if !strings.Contains(v, "disabled: not in Caddy's config") {
		t.Errorf("details of a disabled site:\n%s", v)
	}
	// Moving never wraps.
	m.handleKey(keyCP)
	if m.cursor != 0 {
		t.Errorf("C-p on the first row moved to %d", m.cursor)
	}
	m.handleKey(keyCN)
	m.handleKey(keyCN)
	if m.cursor != 1 {
		t.Errorf("C-n past the end: cursor %d", m.cursor)
	}
	if v, _ := view(m); !strings.Contains(v, "https://b.home.arpa") || !strings.Contains(v, "reverse_proxy localhost:8096") ||
		!strings.Contains(v, "Service: jellyfin.service (user)  ● active") {
		t.Errorf("details of b:\n%s", v)
	}
	// Narrow terminals drop columns, never the site name.
	m.w = 50
	if _, lines := view(m); !strings.Contains(lines[4], "b.home.arpa") {
		t.Errorf("narrow row = %q", lines[4])
	}
	for _, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		if ansi.StringWidth(l) > m.w {
			t.Errorf("line wider than %d: %q", m.w, l)
		}
	}
}

func TestEmptyList(t *testing.T) {
	m := testModel(t)
	m.statusKnown, m.caddy = true, caddy.Status{}
	v, _ := view(m)
	if !strings.Contains(v, "No sites yet") || !strings.Contains(v, "Caddy: not installed (M-x Check Caddy setup)") {
		t.Errorf("empty list:\n%s", v)
	}
	m.handleKey(runes("e"))
	if !m.msgErr || !strings.Contains(m.msg, "No site selected") {
		t.Errorf("e with no sites: %q", m.msg)
	}
}

func TestFormShowsFieldsForServes(t *testing.T) {
	m := testModel(t)
	m.handleKey(runes("c"))
	if m.mode != modeForm {
		t.Fatal("c didn't open the form")
	}
	v, lines := view(m)
	if !strings.Contains(lines[0], "Add Site") {
		t.Errorf("top bar = %q", lines[0])
	}
	if !strings.Contains(v, "Upstream") || strings.Contains(v, "Directory") {
		t.Errorf("proxy form shows the wrong fields:\n%s", v)
	}
	if len(lines) != m.h {
		t.Errorf("view is %d lines, want %d", len(lines), m.h)
	}
	// Service first, then Name, Serves, Upstream/Directory, Redirect, HTTPS,
	// Trusted only, Enabled.
	var ids []string
	for _, fl := range m.form.fields {
		ids = append(ids, fl.id)
	}
	if got := strings.Join(ids, " "); got != "service name serves upstream root redirect https trusted enabled" {
		t.Errorf("field order = %s", got)
	}
	if m.form.fields[m.form.focus].id != "service" {
		t.Errorf("the form opens on %q, want service", m.form.fields[m.form.focus].id)
	}
	m.handleKey(keyCN) // Name
	m.handleKey(runes("jellyfin"))
	if v, _ := view(m); !strings.Contains(v, "Served at https://jellyfin.home.arpa") {
		t.Errorf("name help doesn't show the address:\n%s", v)
	}
	m.handleKey(keyCN) // Serves
	m.handleKey(runes(" "))
	v, _ = view(m)
	if strings.Contains(v, "Upstream") || !strings.Contains(v, "Directory") {
		t.Errorf("static form shows the wrong fields:\n%s", v)
	}
	m.handleKey(keyCN)
	if m.form.fields[m.form.focus].id != "root" {
		t.Errorf("after Serves, focus is on %q, want root", m.form.fields[m.form.focus].id)
	}
	m.handleKey(keyCN)
	if m.form.fields[m.form.focus].id != "redirect" {
		t.Errorf("after Directory, focus is on %q, want redirect", m.form.fields[m.form.focus].id)
	}
	// Moving down stops on the last field.
	for range m.form.fields {
		m.handleKey(keyCN)
	}
	if id := m.form.fields[m.form.focus].id; id != "enabled" {
		t.Errorf("focus after the end = %q", id)
	}
	m.handleKey(keyCG)
	if m.mode != modeList || m.form != nil {
		t.Error("C-g didn't close the form")
	}
}

func TestFormSaveAndEdit(t *testing.T) {
	m := testModel(t)
	m.handleKey(runes("c"))
	m.handleKey(keyCN) // Name
	m.handleKey(runes("app"))
	// C-c C-c with no upstream: refused, form stays.
	m.handleKey(keyCC)
	m.handleKey(keyCC)
	if m.form == nil || !strings.Contains(m.form.err, "Upstream is required") {
		t.Fatalf("missing upstream accepted (err %q)", m.form.err)
	}
	m.handleKey(keyCP) // Service
	if v, _ := view(m); !strings.Contains(v, "None ✓") || !strings.Contains(v, "1 of 3") {
		t.Errorf("service field:\n%s", v)
	}
	m.handleKey(runes(" ")) // jellyfin.service (user): fills Upstream from its health URL
	if got := m.form.val("upstream"); got != "localhost:8096" {
		t.Errorf("upstream from the service's URL = %q", got)
	}
	m.handleKey(keyCC)
	run(m, m.handleKey(keyCC))
	if m.form != nil || m.mode != modeList {
		t.Fatalf("form still open: %q / %q", m.msg, m.form.err)
	}
	c, _ := config.Load()
	if len(c.Sites) != 1 || c.Sites[0].Upstream != "localhost:8096" || !c.Sites[0].HTTPS || !c.Sites[0].TrustedOnly || !c.Sites[0].Enabled ||
		c.Sites[0].Service != "jellyfin.service" || c.Sites[0].ServiceScope != "user" {
		t.Fatalf("saved sites = %+v", c.Sites)
	}
	if !strings.Contains(m.msg, "Add app.home.arpa: saved") || !strings.Contains(m.msg, "caddy isn't installed") ||
		!strings.Contains(m.msg, "apply with C-x C-s") {
		t.Errorf("message = %q", m.msg)
	}
	if _, err := os.Stat(c.CaddyFile(c.Sites[0])); err == nil {
		t.Error("saving a site reached Caddy's directory")
	}
	b, err := os.ReadFile(c.LocalFile(c.Sites[0]))
	if err != nil || !strings.Contains(string(b), "app.home.arpa {") || !strings.Contains(string(b), "import trusted_only") {
		t.Errorf("site file (%v):\n%s", err, b)
	}

	// Edit: rename it.
	m.handleKey(runes("e"))
	if m.form == nil || m.form.orig != "app.home.arpa" || m.form.val("upstream") != "localhost:8096" || m.form.val("service") != "user/jellyfin.service" {
		t.Fatalf("edit form = %+v", m.form)
	}
	m.handleKey(keyCN) // Name (the cursor at its start)
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlK})
	m.handleKey(runes("web"))
	m.handleKey(keyCC)
	run(m, m.handleKey(keyCC))
	c, _ = config.Load()
	if len(c.Sites) != 1 || c.Sites[0].Name != "web" {
		t.Fatalf("after rename: %+v", c.Sites)
	}
	if _, err := os.Stat(filepath.Join(config.SitesDir(), "app.home.arpa.caddy")); err == nil {
		t.Error("the old name's file is still there")
	}
	if s := m.selected(); s == nil || s.Name != "web" {
		t.Errorf("cursor isn't on the renamed site: %+v", s)
	}

	// Disable it, then delete it.
	run(m, m.handleKey(runes("x")))
	c, _ = config.Load()
	if c.Sites[0].Enabled {
		t.Error("x didn't disable the site")
	}
	if _, err := os.Stat(c.LocalFile(c.Sites[0])); err == nil {
		t.Error("a disabled site still has its file")
	}
	m.handleKey(runes("d"))
	if m.prompt == nil {
		t.Fatal("delete didn't ask")
	}
	m.handleKey(runes("yes"))
	run(m, m.handleKey(tea.KeyMsg{Type: tea.KeyEnter}))
	c, _ = config.Load()
	if len(c.Sites) != 0 {
		t.Errorf("after delete: %+v", c.Sites)
	}
}

func TestPaletteAndHelp(t *testing.T) {
	m := testModel(t)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x"), Alt: true})
	if !m.paletteOpen() {
		t.Fatal("M-x didn't open the palette")
	}
	m.handleKey(runes("hosts"))
	if v, _ := view(m); !strings.Contains(v, "Show hosts lines") || strings.Contains(v, "Add site") {
		t.Errorf("palette filter:\n%s", v)
	}
	m.handleKey(keyCG)
	m.handleKey(runes("?"))
	if m.mode != modePager || m.pager.title != "Keybindings" {
		t.Fatal("? didn't open the help")
	}
	if v, _ := view(m); !strings.Contains(v, "Delete site") || !strings.Contains(v, "Show site file") {
		t.Errorf("help:\n%s", v)
	}
	m.handleKey(runes("q"))
	if m.mode != modeList {
		t.Error("q didn't leave the help")
	}
}

func TestPendingChanges(t *testing.T) {
	m := testModel(t, config.Site{Name: "a", Enabled: true, Kind: config.Proxy, Upstream: "1"})
	// Nothing to apply: C-x C-s says so, q quits right away.
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlX})
	if cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlS}); cmd != nil || m.msg != "No changes to apply" {
		t.Errorf("C-x C-s with nothing pending: %q", m.msg)
	}
	if cmd := m.handleKey(runes("q")); cmd == nil || m.prompt != nil {
		t.Error("q with nothing pending didn't quit")
	}

	m.pending = caddy.PendingSet{Added: []string{"a.home.arpa"}, Removed: []string{"gone.home.arpa"}}
	_, lines := view(m)
	if !strings.Contains(lines[0], "2 changes to apply (C-x C-s)") {
		t.Errorf("top bar = %q", lines[0])
	}
	if !strings.Contains(lines[3], "a.home.arpa ✎") {
		t.Errorf("row = %q", lines[3])
	}
	// q asks first.
	if cmd := m.handleKey(runes("q")); cmd != nil || m.prompt == nil || !strings.Contains(m.prompt.label, "2 changes aren't applied") {
		t.Fatalf("q with pending changes didn't ask (prompt %+v)", m.prompt)
	}
	m.handleKey(keyCG)
	// C-x C-s starts Apply changes; without caddy installed it fails and says why.
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlX})
	cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil || m.busy == "" {
		t.Fatal("C-x C-s didn't start applying")
	}
	m.update(cmd())
	if m.busy != "" || !m.msgErr || !strings.Contains(m.msg, "Apply changes failed: caddy isn't installed") {
		t.Errorf("after apply: busy %q, msg %q", m.busy, m.msg)
	}
}
