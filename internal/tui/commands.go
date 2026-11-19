package tui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"caddyman/internal/caddy"
	"caddyman/internal/config"
	"caddyman/internal/hosts"
)

// command is a named action, reachable by key and by M-x.
type command struct {
	name     string
	keys     string // space-separated bubbletea key names
	desc     string
	needSite bool
	run      func(m *Model, s *config.Site) tea.Cmd
}

var commands []command

func init() {
	commands = []command{
		{"Add site", "c", "Add a site: a name, the port (or directory) it serves and the service behind it", false, func(m *Model, _ *config.Site) tea.Cmd { return m.openForm(nil) }},
		{"Edit site", "enter e", "Change the site's settings in a form", true, func(m *Model, s *config.Site) tea.Cmd { return m.openForm(s) }},
		{"Disable or enable site", "x", "Take the site out of Caddy's config, or put it back (it stays in the list)", true, func(m *Model, s *config.Site) tea.Cmd { return m.toggleSite(*s) }},
		{"Delete site", "ctrl+k d", "Remove the site and its file (asks first)", true, func(m *Model, s *config.Site) tea.Cmd {
			m.confirmDelete(*s)
			return nil
		}},
		{"Open in browser", "o", "Open the site in your browser (xdg-open)", true, func(m *Model, s *config.Site) tea.Cmd {
			u := s.URL(m.cfg.DomainSuffix)
			if err := exec.Command("xdg-open", u).Start(); err != nil {
				m.setErr("xdg-open: %v — URL: %s", err, u)
			} else {
				m.setMsg("Opened %s", u)
			}
			return nil
		}},
		{"Show site file", "v", "Show the site's file in CaddyMan's directory, and whether Caddy has it", true, func(m *Model, s *config.Site) tea.Cmd { return m.showSiteFile(*s) }},
		{"Apply changes", "", "Copy the saved changes into Caddy's directory and reload Caddy (C-x C-s; asks for your password)", false, func(m *Model, _ *config.Site) tea.Cmd {
			return m.applyChanges(false)
		}},
		{"Reload Caddy", "r", "Apply changes even when there are none, so Caddy reloads its config (asks for your password)", false, func(m *Model, _ *config.Site) tea.Cmd {
			return m.applyChanges(true)
		}},
		{"Caddy status", "i", "Show systemd's status of " + caddy.UnitName + " and its last log lines", false, func(m *Model, _ *config.Site) tea.Cmd {
			return m.showStatic("Caddy status", caddy.UnitName,
				exec.Command("systemctl", "status", "--no-pager", "-l", "-n", "30", "--", caddy.UnitName))
		}},
		{"Service status", "s", "Show systemd's status of the service behind the site", true, func(m *Model, s *config.Site) tea.Cmd {
			if s.Service == "" {
				m.setErr("%s has no service (e, then Service, links one)", m.cfg.Address(*s))
				return nil
			}
			args := []string{"status", "--no-pager", "-l", "-n", "30", "--", s.Service}
			if s.ServiceScope == "user" {
				args = append([]string{"--user"}, args...)
			}
			return m.showStatic("Service status", s.Service, exec.Command("systemctl", args...))
		}},
		{"Check Caddy setup", "", "Check that Caddy runs and imports CaddyMan's sites, and say how to fix what doesn't", false, func(m *Model, _ *config.Site) tea.Cmd { return m.showCheck() }},
		{"Show hosts lines", "", "Lines for /etc/hosts on your devices, so they find the sites without local DNS", false, func(m *Model, _ *config.Site) tea.Cmd { return m.showHosts() }},
		{"Show root certificate", "", "Where Caddy's root certificate is and how to trust it on your devices (needed for HTTPS sites)", false, func(m *Model, _ *config.Site) tea.Cmd { return m.showRootCert() }},
		{"Filter", "ctrl+s", "Show only the sites that match what you type", false, func(m *Model, _ *config.Site) tea.Cmd { return m.startFilter() }},
		{"Refresh", "g", "Check Caddy and the services and re-read the config now", false, func(m *Model, _ *config.Site) tea.Cmd {
			m.setMsg("Refreshed")
			return m.refreshCmd()
		}},
		{"Run command", "alt+x", "Run a command by name", false, func(m *Model, _ *config.Site) tea.Cmd { return m.openPalette() }},
		{"Help", "? ctrl+h", "Show the keybindings", false, func(m *Model, _ *config.Site) tea.Cmd { return m.showHelp() }},
		{"Quit", "q", "Quit CaddyMan (also C-x C-c)", false, func(m *Model, _ *config.Site) tea.Cmd { return m.quit() }},
	}
}

func runCommand(m *Model, c command) tea.Cmd {
	s := m.selected()
	if c.needSite && s == nil {
		m.setErr("No site selected")
		return nil
	}
	return c.run(m, s)
}

func (m *Model) toggleSite(s config.Site) tea.Cmd {
	addr := m.cfg.Address(s)
	verb := "Disable"
	if !s.Enabled {
		verb = "Enable"
	}
	return m.saveChange(verb+" "+addr, func(c *config.Config) error {
		t := c.Find(addr)
		if t == nil {
			return fmt.Errorf("%s is no longer in the config", addr)
		}
		t.Enabled = !s.Enabled
		return nil
	}, nil, nil)
}

func (m *Model) confirmDelete(s config.Site) {
	addr := m.cfg.Address(s)
	m.yesOrNo("Delete "+addr+"?", func(m *Model) tea.Cmd {
		return m.saveChange("Delete "+addr, func(c *config.Config) error {
			if !c.Remove(addr) {
				return fmt.Errorf("%s is no longer in the config", addr)
			}
			return nil
		}, nil, func(m *Model) { m.clampCursor() })
	})
}

func (m *Model) showSiteFile(s config.Site) tea.Cmd {
	p := m.cfg.LocalFile(s)
	b, err := os.ReadFile(p)
	switch {
	case !s.Enabled:
		m.setErr("%s is disabled, so it has no file (x enables it)", m.cfg.Address(s))
		return nil
	case errors.Is(err, os.ErrNotExist):
		m.setErr("No file at %s yet (e, then C-c C-c, writes it)", config.Tilde(p))
		return nil
	case err != nil:
		m.setErr("%v", err)
		return nil
	}
	note := sGreen.Render("Caddy has this version (" + m.cfg.CaddyFile(s) + ")")
	if cur, err := os.ReadFile(m.cfg.CaddyFile(s)); err != nil {
		note = sYellow.Render("Not applied yet: Caddy doesn't have this site (C-x C-s applies it)")
	} else if string(cur) != string(b) {
		note = sYellow.Render("Not applied yet: Caddy has an older version (C-x C-s applies it)")
	}
	lines := append([]string{note, ""}, strings.Split(strings.TrimRight(string(b), "\n"), "\n")...)
	return m.showText("Site file", config.Tilde(p), lines)
}

// showCheck runs `caddyman check` in the pager; it runs caddy, so off the
// UI goroutine.
func (m *Model) showCheck() tea.Cmd {
	p := newPager("Caddy setup", "")
	p.loading = true
	m.pager, m.mode = p, modePager
	c := m.cfg
	return func() tea.Msg { return staticLoadedMsg{p, checkLines(caddy.CheckSetup(c))} }
}

func checkLines(checks []caddy.Check) []string {
	var out []string
	for _, c := range checks {
		if c.OK {
			out = append(out, sGreen.Render("✓ ")+c.Text)
			continue
		}
		out = append(out, sRed.Render("✗ ")+c.Text)
		if c.Fix != "" {
			out = append(out, "  "+sDim.Render("→ "+c.Fix))
		}
	}
	return out
}

func (m *Model) showHosts() tea.Cmd {
	ip, err := hosts.LanIP(m.cfg.LanIP)
	if err != nil {
		m.setErr("%v", err)
		return nil
	}
	lines := []string{
		sDim.Render("# Add these to /etc/hosts on each device (on Windows:"),
		sDim.Render("# C:\\Windows\\System32\\drivers\\etc\\hosts) until a local DNS server answers for them."),
		sDim.Render("# The address is this machine's (lan_ip in the config overrides it)."),
		"",
	}
	hl := hosts.Lines(m.cfg, ip)
	if len(hl) == 0 {
		lines = append(lines, sDim.Render("(no enabled sites)"))
	}
	return m.showText("Hosts lines", ip, append(lines, hl...))
}

func (m *Model) showRootCert() tea.Cmd {
	p := m.cfg.RootCertPath()
	lines := []string{sBold.Render("Root certificate of Caddy's own certificate authority"), "", "  " + p}
	rc, err := caddy.ReadRootCert(p)
	switch {
	case errors.Is(err, os.ErrNotExist):
		lines = append(lines, "", sYellow.Render("  Not there: Caddy makes it when it first serves an HTTPS site (or root_cert in the config points elsewhere)."))
	case errors.Is(err, os.ErrPermission):
		lines = append(lines, "",
			sYellow.Render("  Only Caddy's user can read it. Copy it out once:"),
			"  sudo cp "+p+" ~/caddy-root.crt && sudo chown $USER ~/caddy-root.crt")
	case err != nil:
		lines = append(lines, "", sRed.Render("  "+err.Error()))
	default:
		lines = append(lines,
			"",
			"  Name:     "+rc.Subject,
			"  Expires:  "+rc.NotAfter.Format("2006-01-02"),
			"  SHA-256:  "+rc.SHA256)
	}
	lines = append(lines,
		"",
		sBold.Render("Trusting it"),
		"",
		"  Browsers accept the HTTPS sites once the device trusts this certificate.",
		"  Copy root.crt to the device, compare its SHA-256 with the one above, then:",
		"",
		"  Linux (Debian/Ubuntu)  sudo cp root.crt /usr/local/share/ca-certificates/caddy-home.crt",
		"                         sudo update-ca-certificates",
		"  Firefox                Settings → Privacy & Security → Certificates → View Certificates →",
		"                         Authorities → Import",
		"  Android                Settings → Security → Encryption & credentials → Install a certificate →",
		"                         CA certificate",
		"  iOS / iPadOS           Open the file, install the profile in Settings, then turn it on in",
		"                         Settings → General → About → Certificate Trust Settings",
		"  Windows                Double-click root.crt → Install Certificate → Local Machine →",
		"                         Trusted Root Certification Authorities",
		"  macOS                  Double-click root.crt, then in Keychain Access set it to Always Trust",
	)
	return m.showText("Root certificate", "", lines)
}

func (m *Model) startFilter() tea.Cmd {
	old := m.filter
	p := newPrompt("Filter: ", m.filter)
	p.onChange = func(m *Model, v string) {
		m.filter = strings.TrimSpace(v)
		m.cursor = 0
	}
	p.onSubmit = func(m *Model, v string) tea.Cmd {
		m.filter = strings.TrimSpace(v)
		m.clampCursor()
		return nil
	}
	p.onCancel = func(m *Model) {
		m.filter = old
		m.clampCursor()
	}
	m.prompt = p
	return nil
}
