package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"caddyman/internal/config"
	"caddyman/internal/serviceinfo"
)

func info(name string, port int, https, trusted bool, redirect string) *serviceinfo.Info {
	i := &serviceinfo.Info{Version: 1, Name: name, Proxy: serviceinfo.Proxy{HTTPS: https, TrustedOnly: trusted, RedirectRoot: redirect}}
	if port > 0 {
		i.Port = &port
	}
	return i
}

func TestNewSiteFilledFromServiceInfo(t *testing.T) {
	m := testModel(t)
	m.openForm(nil)
	f := m.form
	f.get("service").setChoice("user/jellyfin.service")
	f.choiceChanged(m, f.get("service")) // not known as autoconfigurable yet: Upstream from ServiceMan's health URL
	if got := f.val("upstream"); got != "localhost:8096" || f.upAuto {
		t.Fatalf("upstream from the health URL = %q (auto %v)", got, f.upAuto)
	}
	f.get("redirect").ti.SetValue("/mine/") // typed while the binary answers
	m.update(serviceInfoMsg{f, "user/jellyfin.service", info("bookshelf", 20001, false, false, "/web/")})
	for id, want := range map[string]string{"name": "bookshelf", "upstream": "auto", "https": "no", "trusted": "no", "redirect": "/mine/"} {
		if got := f.val(id); got != want {
			t.Errorf("%s = %q, want %q", id, got, want)
		}
	}
	if s := f.site(); !s.IsAuto() || s.Service != "jellyfin.service" {
		t.Errorf("site = %+v", s)
	}
	if v, _ := view(m); !strings.Contains(v, "Follows jellyfin.service's port, now 20001") {
		t.Errorf("auto Upstream not described:\n%s", v)
	}
	m.showPreview = true
	if v, _ := view(m); !strings.Contains(v, "reverse_proxy localhost:20001") {
		t.Errorf("preview doesn't show the service's port:\n%s", v)
	}

	// Auto Upstream is skipped when moving and can't be typed in.
	f.setFocus(2) // Serves
	f.move(1)
	if id := f.fields[f.focus].id; id != "redirect" {
		t.Errorf("after Serves, focus is on %q, want redirect", id)
	}
	f.setFocus(3)
	m.handleKey(runes("9"))
	m.handleKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if f.val("upstream") != "auto" {
		t.Errorf("auto upstream was typed over: %q", f.val("upstream"))
	}

	// Another service: what was filled in follows it; what was typed stays.
	f.setFocus(0)
	f.get("name").ti.SetValue("books")
	f.get("service").setChoice("system/nginx.service")
	f.choiceChanged(m, f.get("service")) // unknown yet: Upstream can be typed again
	if f.upAuto || f.val("upstream") != "" {
		t.Errorf("unknown service: auto %v, upstream %q", f.upAuto, f.val("upstream"))
	}
	m.update(serviceInfoMsg{f, "system/nginx.service", info("nginx", 20002, true, true, "")})
	for id, want := range map[string]string{"name": "books", "upstream": "auto", "https": "yes", "trusted": "yes", "redirect": "/mine/"} {
		if got := f.val(id); got != want {
			t.Errorf("after switching, %s = %q, want %q", id, got, want)
		}
	}

	// A late answer for a service no longer picked is ignored.
	m.update(serviceInfoMsg{f, "user/jellyfin.service", info("late", 20009, false, false, "")})
	if f.val("https") != "yes" || f.autoFrom != "nginx" {
		t.Errorf("late answer applied: https %q, from %q", f.val("https"), f.autoFrom)
	}

	// Back to an autoconfigurable one already known: auto at once. None:
	// Upstream is typed again.
	f.get("service").setChoice("user/jellyfin.service")
	f.choiceChanged(m, f.get("service"))
	if !f.upAuto || f.val("upstream") != "auto" {
		t.Errorf("known autoconfigurable service: auto %v, upstream %q", f.upAuto, f.val("upstream"))
	}
	f.get("service").setChoice(noService)
	f.choiceChanged(m, f.get("service"))
	if f.upAuto || f.val("upstream") != "" {
		t.Errorf("no service: auto %v, upstream %q", f.upAuto, f.val("upstream"))
	}
	f.setFocus(2)
	f.move(1)
	if id := f.fields[f.focus].id; id != "upstream" {
		t.Errorf("Upstream isn't focusable again: focus on %q", id)
	}
}

// An edited site keeps its settings, but Upstream becomes auto when its
// service is autoconfigurable.
func TestEditedSiteUpstreamBecomesAuto(t *testing.T) {
	edit := func(upstream string) (*Model, *form) {
		m := testModel(t, config.Site{Name: "app", Enabled: true, Kind: config.Proxy, Upstream: upstream, HTTPS: true,
			Service: "jellyfin.service", ServiceScope: "user"})
		if cmd := m.openForm(&m.cfg.Sites[0]); cmd == nil {
			t.Fatal("opening an edited site with a service doesn't ask it")
		}
		return m, m.form
	}
	answer := func(m *Model, f *form, port int) {
		m.update(serviceInfoMsg{f, "user/jellyfin.service", info("bookshelf", port, false, false, "/web/")})
	}

	m, f := edit("localhost:9000")
	answer(m, f, 20001)
	if f.val("upstream") != "auto" || f.val("name") != "app" || f.val("https") != "yes" || f.val("redirect") != "" {
		t.Errorf("upstream %q name %q https %q redirect %q", f.val("upstream"), f.val("name"), f.val("https"), f.val("redirect"))
	}
	if v, _ := view(m); !strings.Contains(v, "autoconfigured from bookshelf") || !strings.Contains(m.msg, "localhost:9000 → auto") {
		t.Errorf("no word of the change: msg %q\n%s", m.msg, v)
	}

	// Already auto: locked from the start, nothing to say.
	m, f = edit("auto")
	if !f.upAuto || f.val("upstream") != "auto" {
		t.Errorf("auto site opened with auto %v, upstream %q", f.upAuto, f.val("upstream"))
	}
	if v, _ := view(m); !strings.Contains(v, "Follows jellyfin.service's port (asking it…)") {
		t.Errorf("auto Upstream before the answer:\n%s", v)
	}
	m.msg = ""
	answer(m, f, 20001)
	if m.msg != "" {
		t.Errorf("msg = %q", m.msg)
	}

	m, f = edit("")
	answer(m, f, 0)
	if f.val("upstream") != "auto" || !strings.Contains(m.msg, "no port yet") {
		t.Errorf("no port: upstream %q, msg %q", f.val("upstream"), m.msg)
	}
	if v, _ := view(m); !strings.Contains(v, "it has none yet: start it once") {
		t.Errorf("no port not shown:\n%s", v)
	}
}

// The Service field marks the autoconfigurable services, with their port.
func TestServiceFieldMarksAutoconfigurable(t *testing.T) {
	m := testModel(t)
	m.openForm(nil)
	m.update(serviceInfosMsg{"user/jellyfin.service": info("jellyfin", 8096, true, true, ""), "system/nginx.service": info("nginx", 0, true, true, "")})
	fl := m.form.get("service")
	want := []string{"None", "jellyfin.service (user) — autoconfigurable, port 8096", "nginx.service (system) — autoconfigurable, no port yet (start it once)"}
	if strings.Join(fl.labels, "|") != strings.Join(want, "|") {
		t.Errorf("labels = %q, want %q", fl.labels, want)
	}
	if !strings.Contains(fl.help, "autoconfigurable ones fill in the site") {
		t.Errorf("help = %q", fl.help)
	}

	// A new site picked without a port gets everything else, and word why.
	f := m.form
	f.get("service").setChoice("system/nginx.service")
	m.update(serviceInfoMsg{f, "system/nginx.service", info("nginx", 0, false, true, "/x/")})
	if f.val("name") != "nginx" || f.val("upstream") != "auto" || f.val("redirect") != "/x/" || !strings.Contains(m.msg, "no port yet") {
		t.Errorf("name %q upstream %q redirect %q msg %q", f.val("name"), f.val("upstream"), f.val("redirect"), m.msg)
	}
}

// Infos arriving after a service was picked make its Upstream auto.
func TestServiceInfosMakeUpstreamAuto(t *testing.T) {
	m := testModel(t)
	m.openForm(nil)
	f := m.form
	f.get("service").setChoice("user/jellyfin.service")
	f.choiceChanged(m, f.get("service"))
	m.update(serviceInfosMsg{"user/jellyfin.service": info("jellyfin", 8096, true, true, "")})
	if !f.upAuto || f.val("upstream") != "auto" {
		t.Errorf("auto %v, upstream %q", f.upAuto, f.val("upstream"))
	}
}
