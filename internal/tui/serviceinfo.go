package tui

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"caddyman/internal/serviceinfo"
	"caddyman/internal/services"
)

// A server with a `caddyman-info` subcommand describes itself as JSON: its
// name, port and suggested HTTPS, Trusted only and Redirect values. Such a
// service is autoconfigurable: the Service field says so, picking it fills
// in a new site, and an existing site's Upstream follows its port.

// serviceInfoMsg carries the answer for the service picked in form f.
type serviceInfoMsg struct {
	f    *form
	key  string // the service's key, "<scope>/<unit>"
	info *serviceinfo.Info
}

// serviceInfosMsg is what each tracked service's binary said, by key (the
// services without caddyman-info are left out).
type serviceInfosMsg map[string]*serviceinfo.Info

// queryServiceInfo asks the binary behind unit u, off the UI goroutine.
func queryServiceInfo(f *form, u services.Unit, cache *serviceinfo.Cache) tea.Cmd {
	return func() tea.Msg {
		bin := services.Binary(u)
		if bin == "" {
			return nil
		}
		info, err := cache.Query(bin)
		if err != nil {
			return nil
		}
		return serviceInfoMsg{f, u.Key(), info}
	}
}

// serviceInfosCmd asks every tracked service's binary, off the UI goroutine,
// to mark the autoconfigurable ones in the Service field.
func (m *Model) serviceInfosCmd() tea.Cmd {
	units := append([]services.Unit(nil), m.tracked...)
	m.infoFor = trackedKeys(units)
	cache := m.infoCache
	return func() tea.Msg {
		out := serviceInfosMsg{}
		for _, u := range units {
			if bin := services.Binary(u); bin != "" {
				if info, err := cache.Query(bin); err == nil {
					out[u.Key()] = info
				}
			}
		}
		return out
	}
}

// trackedKeys identifies a list of tracked services.
func trackedKeys(units []services.Unit) string {
	keys := make([]string, len(units))
	for i, u := range units {
		keys[i] = u.Key()
	}
	return strings.Join(keys, "\n")
}

// applyServiceInfo fills the form when it's still open on the same service.
// A new site takes the name, upstream and suggestions in every field still
// as it was opened (or as last filled in). An edited one keeps its settings,
// but its Upstream follows the service's port: an empty one is filled in,
// and a local one (a port, or localhost:port) that differs from it is
// changed, unless it was changed by hand. Anything typed is kept.
func (m *Model) applyServiceInfo(msg serviceInfoMsg) {
	f, info := msg.f, msg.info
	if m.form != f || m.mode != modeForm || f.val("service") != msg.key {
		return
	}
	if m.info == nil {
		m.info = map[string]*serviceinfo.Info{}
	}
	m.info[msg.key] = info
	f.get("service").labelServices(m.tracked, m.info)
	name := info.Name
	if name == "" {
		_, name, _ = strings.Cut(msg.key, "/")
	}
	port := 0
	if info.Port != nil && *info.Port > 0 {
		port = *info.Port
	}
	noPort := fmt.Sprintf("%s has no port yet: start it once (ServiceMan), then pick it again", name)

	if f.orig != "" {
		up := f.val("upstream")
		switch {
		case port == 0:
			if up == "" {
				m.setMsg("%s", noPort)
			}
		case up == "":
			f.fill("upstream", strconv.Itoa(port))
			f.autoFrom = name
			m.setMsg("Upstream filled in from %s %s: port %d", name, serviceinfo.Command, port)
		case f.replaceable("upstream"):
			if p, ok := localPort(up); ok && p != port {
				f.fill("upstream", withPort(up, port))
				f.autoFrom = name
				m.setMsg("Upstream %s → %s: %s's configured port (C-c C-c saves it)", up, f.val("upstream"), name)
			}
		}
		return
	}

	upstream := ""
	if port > 0 {
		upstream = strconv.Itoa(port)
	}
	yesNo := map[bool]string{true: "yes", false: "no"}
	for id, v := range map[string]string{
		"name":     info.Name,
		"upstream": upstream,
		"https":    yesNo[info.Proxy.HTTPS],
		"trusted":  yesNo[info.Proxy.TrustedOnly],
		"redirect": info.Proxy.RedirectRoot,
	} {
		if (v != "" || id == "redirect") && f.replaceable(id) {
			f.fill(id, v)
		}
	}
	f.autoFrom = name
	if port == 0 {
		m.setMsg("Filled in from %s %s; %s", name, serviceinfo.Command, noPort)
		return
	}
	m.setMsg("Filled in from %s %s", name, serviceinfo.Command)
}

// localPort is the port of an upstream on this machine: "8096",
// "localhost:8096", "127.0.0.1:8096", "[::1]:8096" or ":8096".
func localPort(up string) (int, bool) {
	if p, err := strconv.Atoi(up); err == nil {
		return p, p > 0
	}
	host, ps, err := net.SplitHostPort(up)
	if err != nil {
		return 0, false
	}
	switch host {
	case "", "localhost", "127.0.0.1", "::1":
	default:
		return 0, false
	}
	p, err := strconv.Atoi(ps)
	return p, err == nil && p > 0
}

// withPort is a local upstream with its port replaced, in the same form.
func withPort(up string, port int) string {
	if _, err := strconv.Atoi(up); err == nil {
		return strconv.Itoa(port)
	}
	host, _, _ := net.SplitHostPort(up)
	return net.JoinHostPort(host, strconv.Itoa(port))
}
