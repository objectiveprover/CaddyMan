package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"caddyman/internal/config"
	"caddyman/internal/serviceinfo"
	"caddyman/internal/services"
)

// A server with a `caddyman-info` subcommand describes itself as JSON: its
// name, port and suggested HTTPS, Trusted only and Redirect values. Such a
// service is autoconfigurable: the Service field says so, and picking it
// makes the site's Upstream auto (it follows the service's port, looked up
// whenever the site's file is written) and fills in a new site.

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

// applyServiceInfo fills the form when it's still open on the same service:
// Upstream becomes auto, new site or edited one. A new site also takes the
// name and suggestions in every field still as it was opened (or as last
// filled in); anything typed is kept. An edited site keeps its other
// settings.
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
	noPort := fmt.Sprintf("%s has no port yet: start it once (ServiceMan) before saving", name)
	was := f.val("upstream")
	f.setAuto(true)
	f.autoFrom = name

	if f.orig != "" {
		switch {
		case port == 0:
			m.setMsg("Upstream is auto, but %s", noPort)
		case was != config.AutoUpstream:
			m.setMsg("Upstream %s → auto: follows %s's port, now %d (C-c C-c saves it)", dash(was), name, port)
		}
		return
	}

	yesNo := map[bool]string{true: "yes", false: "no"}
	for id, v := range map[string]string{
		"name":     info.Name,
		"https":    yesNo[info.Proxy.HTTPS],
		"trusted":  yesNo[info.Proxy.TrustedOnly],
		"redirect": info.Proxy.RedirectRoot,
	} {
		if (v != "" || id == "redirect") && f.replaceable(id) {
			f.fill(id, v)
		}
	}
	if port == 0 {
		m.setMsg("Filled in from %s %s; %s", name, serviceinfo.Command, noPort)
		return
	}
	m.setMsg("Filled in from %s %s; Upstream follows its port, now %d", name, serviceinfo.Command, port)
}

// previewSite is s as its file will show it: an auto Upstream filled in
// from its service's caddyman-info, when that's known.
func (m *Model) previewSite(s config.Site) config.Site {
	if i := m.info[s.ServiceKey()]; s.IsAuto() && i != nil && i.Port != nil && *i.Port > 0 {
		s.Upstream = "localhost:" + strconv.Itoa(*i.Port)
	}
	return s
}

// dash is v, or "(empty)" when there's nothing.
func dash(v string) string {
	if v == "" {
		return "(empty)"
	}
	return v
}
