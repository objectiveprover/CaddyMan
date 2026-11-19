package tui

import (
	"strings"
	"testing"

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
	f.choiceChanged(m, f.get("service")) // Upstream from ServiceMan's health URL first
	if got := f.val("upstream"); got != "localhost:8096" {
		t.Fatalf("upstream from the health URL = %q", got)
	}
	f.get("redirect").ti.SetValue("/mine/") // typed while the binary answers
	m.update(serviceInfoMsg{f, "user/jellyfin.service", info("bookshelf", 20001, false, false, "/web/")})
	for id, want := range map[string]string{"name": "bookshelf", "upstream": "20001", "https": "no", "trusted": "no", "redirect": "/mine/"} {
		if got := f.val(id); got != want {
			t.Errorf("%s = %q, want %q", id, got, want)
		}
	}

	// Another service: what was filled in follows it; what was typed stays.
	f.get("name").ti.SetValue("books")
	f.get("service").setChoice("system/nginx.service")
	m.update(serviceInfoMsg{f, "system/nginx.service", info("nginx", 20002, true, true, "")})
	for id, want := range map[string]string{"name": "books", "upstream": "20002", "https": "yes", "trusted": "yes", "redirect": "/mine/"} {
		if got := f.val(id); got != want {
			t.Errorf("after switching, %s = %q, want %q", id, got, want)
		}
	}

	// A late answer for a service no longer picked is ignored.
	m.update(serviceInfoMsg{f, "user/jellyfin.service", info("late", 20009, false, false, "")})
	if f.val("upstream") != "20002" {
		t.Errorf("late answer applied: upstream %q", f.val("upstream"))
	}
}

// An edited site keeps its settings, but its Upstream follows the service's
// port: an empty one or a stale local one is set, one typed by hand or on
// another host is kept.
func TestEditedSiteUpstreamFollowsPort(t *testing.T) {
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
	if f.val("upstream") != "localhost:20001" || f.val("name") != "app" || f.val("https") != "yes" || f.val("redirect") != "" {
		t.Errorf("stale upstream: upstream %q name %q https %q redirect %q", f.val("upstream"), f.val("name"), f.val("https"), f.val("redirect"))
	}
	if v, _ := view(m); !strings.Contains(v, "autoconfigured from bookshelf") || !strings.Contains(m.msg, "localhost:9000 → localhost:20001") {
		t.Errorf("no word of the change: msg %q\n%s", m.msg, v)
	}

	for _, up := range []string{"9000", "localhost:20001"} {
		m, f = edit(up)
		answer(m, f, 20001)
		if want := map[string]string{"9000": "20001", "localhost:20001": "localhost:20001"}[up]; f.val("upstream") != want {
			t.Errorf("%s: upstream %q, want %q", up, f.val("upstream"), want)
		}
	}

	m, f = edit("localhost:9000")
	f.get("upstream").ti.SetValue("localhost:7000") // typed
	answer(m, f, 20001)
	if f.val("upstream") != "localhost:7000" {
		t.Errorf("typed upstream replaced: %q", f.val("upstream"))
	}

	m, f = edit("nas:9000")
	answer(m, f, 20001)
	if f.val("upstream") != "nas:9000" {
		t.Errorf("remote upstream replaced: %q", f.val("upstream"))
	}

	m, f = edit("")
	answer(m, f, 0)
	if f.val("upstream") != "" || !strings.Contains(m.msg, "no port yet") {
		t.Errorf("no port: upstream %q, msg %q", f.val("upstream"), m.msg)
	}
	answer(m, f, 20001)
	if f.val("upstream") != "20001" {
		t.Errorf("empty upstream not filled: %q", f.val("upstream"))
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
	if f.val("name") != "nginx" || f.val("upstream") != "" || f.val("redirect") != "/x/" || !strings.Contains(m.msg, "no port yet") {
		t.Errorf("name %q upstream %q redirect %q msg %q", f.val("name"), f.val("upstream"), f.val("redirect"), m.msg)
	}
}

func TestLocalPort(t *testing.T) {
	for up, want := range map[string]int{"8096": 8096, "localhost:8096": 8096, "127.0.0.1:1": 1, "[::1]:9": 9, ":80": 80, "nas:8096": 0, "localhost": 0, "": 0} {
		if p, ok := localPort(up); p != want && ok || ok != (want > 0) {
			t.Errorf("localPort(%q) = %d, %v", up, p, ok)
		}
	}
	if got := withPort("[::1]:9", 20001); got != "[::1]:20001" {
		t.Errorf("withPort = %q", got)
	}
}
