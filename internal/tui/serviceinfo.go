package tui

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"caddyman/internal/serviceinfo"
	"caddyman/internal/services"
)

// Picking a service in the site form asks its binary for `service-info`
// (a server that has it describes itself as JSON): its name, port and
// suggested HTTPS, Trusted only and Redirect values fill in the form.

// serviceInfoMsg carries the answer for the service picked in form f.
type serviceInfoMsg struct {
	f    *form
	key  string // the service's key, "<scope>/<unit>"
	info *serviceinfo.Info
}

// queryServiceInfo asks the binary behind unit u, off the UI goroutine.
func queryServiceInfo(f *form, u services.Unit) tea.Cmd {
	return func() tea.Msg {
		bin := services.Binary(u)
		if bin == "" {
			return nil
		}
		info, err := serviceinfo.Query(bin)
		if err != nil {
			return nil
		}
		return serviceInfoMsg{f, u.Key(), info}
	}
}

// applyServiceInfo fills the form when it's still open on the same service.
// A new site takes the name, upstream and suggestions in every field still
// as it was opened (or as last filled in); an edited one only an empty
// Upstream. Anything typed is kept.
func (m *Model) applyServiceInfo(msg serviceInfoMsg) {
	f, info := msg.f, msg.info
	if m.form != f || m.mode != modeForm || f.val("service") != msg.key {
		return
	}
	upstream := ""
	if info.Port != nil && *info.Port > 0 {
		upstream = strconv.Itoa(*info.Port)
	}
	if f.orig != "" {
		if upstream != "" && f.val("upstream") == "" {
			f.fill("upstream", upstream)
		}
		return
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
	m.setMsg("Filled in from %s service-info", info.Name)
}
