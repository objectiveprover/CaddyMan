package tui

import (
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

func TestEditedSiteOnlyGetsAnEmptyUpstream(t *testing.T) {
	m := testModel(t, config.Site{Name: "app", Enabled: true, Kind: config.Proxy, Upstream: "localhost:9000", HTTPS: true})
	m.openForm(&m.cfg.Sites[0])
	f := m.form
	f.get("service").setChoice("user/jellyfin.service")
	f.choiceChanged(m, f.get("service"))
	m.update(serviceInfoMsg{f, "user/jellyfin.service", info("bookshelf", 20001, false, false, "/web/")})
	if f.val("upstream") != "localhost:9000" || f.val("name") != "app" || f.val("https") != "yes" || f.val("redirect") != "" {
		t.Errorf("edited site changed: upstream %q name %q https %q redirect %q", f.val("upstream"), f.val("name"), f.val("https"), f.val("redirect"))
	}
	f.get("upstream").ti.SetValue("")
	m.update(serviceInfoMsg{f, "user/jellyfin.service", info("bookshelf", 20001, false, false, "")})
	if f.val("upstream") != "20001" {
		t.Errorf("empty upstream not filled: %q", f.val("upstream"))
	}
}
