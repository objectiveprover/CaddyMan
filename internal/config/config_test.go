package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTemplateMatchesDefaults(t *testing.T) {
	b, err := os.ReadFile("../../config.template.json")
	if err != nil {
		t.Fatal(err)
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatalf("template is not a valid config: %v", err)
	}
	if c.Version != CurrentVersion {
		t.Errorf("template version %d, CurrentVersion %d: bump both together", c.Version, CurrentVersion)
	}
	// Every setting is in the template, with the built-in default.
	tmpl := c
	c.applyDefaults()
	if !reflect.DeepEqual(tmpl, c) {
		t.Errorf("template %+v differs from the built-in defaults %+v", tmpl, c)
	}
	var raw map[string]any
	json.Unmarshal(b, &raw)
	out, _ := json.Marshal(Config{})
	var fields map[string]any
	json.Unmarshal(out, &fields)
	for k := range fields {
		if _, ok := raw[k]; !ok {
			t.Errorf("setting %q is missing from the template", k)
		}
	}
}

func TestLoadSaveKeepsTilde(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Version != CurrentVersion || c.DomainSuffix != "home.arpa" {
		t.Fatalf("defaults = %+v", c)
	}
	c.Put("", Site{Name: "a", Kind: Proxy, Upstream: "80", Enabled: true})
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(Path())
	if !strings.Contains(string(b), `"serviceman_config": "~/.config/serviceman/config.json"`) {
		t.Errorf("saved config expanded ~:\n%s", b)
	}
	if !strings.HasPrefix(c.ServicemanConfigPath(), "/") {
		t.Errorf("ServicemanConfigPath = %q", c.ServicemanConfigPath())
	}
	if got := c.CaddyFile(c.Sites[0]); got != "/etc/caddy/caddyman/a.home.arpa.caddy" {
		t.Errorf("CaddyFile = %q", got)
	}
	if got, want := c.LocalFile(c.Sites[0]), filepath.Join(Dir(), "sites", "a.home.arpa.caddy"); got != want {
		t.Errorf("LocalFile = %q, want %q", got, want)
	}
}

func TestAddress(t *testing.T) {
	for _, tc := range []struct{ name, suffix, want string }{
		{"jellyfin", "home.arpa", "jellyfin.home.arpa"},
		{"Jellyfin", "home.arpa.", "jellyfin.home.arpa"},
		{"media.lan", "home.arpa", "media.lan"},
		{"x", "", "x"},
	} {
		if got := (Site{Name: tc.name}).Address(tc.suffix); got != tc.want {
			t.Errorf("Address(%q, %q) = %q, want %q", tc.name, tc.suffix, got, tc.want)
		}
	}
}

func TestPutRemoveSorted(t *testing.T) {
	c := &Config{DomainSuffix: "home.arpa"}
	c.Put("", Site{Name: "b"})
	c.Put("", Site{Name: "a"})
	c.Put("b.home.arpa", Site{Name: "c"}) // rename
	if got := []string{c.Sites[0].Name, c.Sites[1].Name}; !reflect.DeepEqual(got, []string{"c", "a"}) {
		t.Errorf("sites = %v", got)
	}
	if s := c.Sorted(); s[0].Name != "a" {
		t.Errorf("Sorted = %v", s)
	}
	if !c.Remove("a.home.arpa") || c.Remove("a.home.arpa") || len(c.Sites) != 1 {
		t.Errorf("Remove: %v", c.Sites)
	}
}

func TestValidation(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	os.WriteFile(file, nil, 0o644)

	for _, tc := range []struct {
		up string
		ok bool
	}{
		{"8096", true}, {"localhost:8096", true}, {"192.168.1.5:80", true},
		{"http://localhost:3000", true}, {"https://nas:5001", true}, {"[::1]:80", true},
		{"", false}, {"localhost", false}, {"localhost:0", false}, {"localhost:99999", false},
		{"ftp://x:1", false}, {"70000", false}, {"local host:80", false},
	} {
		if err := ValidUpstream(tc.up); (err == nil) != tc.ok {
			t.Errorf("ValidUpstream(%q) = %v, want ok=%v", tc.up, err, tc.ok)
		}
	}
	for _, tc := range []struct {
		name string
		ok   bool
	}{
		{"jellyfin", true}, {"my-app", true}, {"a.b.c", true}, {"Media", true},
		{"", false}, {"-a", false}, {"a-", false}, {"a b", false}, {"a..b", false}, {"a_b", false},
	} {
		if err := ValidName(tc.name); (err == nil) != tc.ok {
			t.Errorf("ValidName(%q) = %v, want ok=%v", tc.name, err, tc.ok)
		}
	}
	for _, tc := range []struct {
		root string
		ok   bool
	}{{dir, true}, {"", false}, {"rel/dir", false}, {file, false}, {filepath.Join(dir, "none"), false}} {
		if err := ValidRoot(tc.root); (err == nil) != tc.ok {
			t.Errorf("ValidRoot(%q) = %v, want ok=%v", tc.root, err, tc.ok)
		}
	}
	for _, tc := range []struct {
		r  string
		ok bool
	}{{"", true}, {"/web/", true}, {"https://x.home.arpa/", true}, {"web", false}, {"/a b", false}, {"/", false}} {
		if err := ValidRedirect(tc.r); (err == nil) != tc.ok {
			t.Errorf("ValidRedirect(%q) = %v, want ok=%v", tc.r, err, tc.ok)
		}
	}

	c := &Config{DomainSuffix: "home.arpa", Sites: []Site{{Name: "a", Kind: Proxy, Upstream: "1"}}}
	if err := c.Validate("", Site{Name: "A", Kind: Proxy, Upstream: "2"}); err == nil {
		t.Error("a duplicate address was accepted")
	}
	if err := c.Validate("a.home.arpa", Site{Name: "a", Kind: Proxy, Upstream: "2"}); err != nil {
		t.Errorf("editing a site in place: %v", err)
	}
	if err := c.Validate("", Site{Name: "s", Kind: Static, Root: dir}); err != nil {
		t.Errorf("static site: %v", err)
	}
	if err := c.Validate("", Site{Name: "t", Kind: Proxy, Upstream: "1", Service: "x.service", ServiceScope: "user"}); err != nil {
		t.Errorf("site with a service: %v", err)
	}
	if err := c.Validate("", Site{Name: "u", Kind: Proxy, Upstream: "1", Service: "x.service"}); err == nil {
		t.Error("a service without a scope was accepted")
	}
	if err := c.Validate("", Site{Name: "v", Kind: Proxy, Upstream: AutoUpstream, Service: "x.service", ServiceScope: "user"}); err != nil {
		t.Errorf("auto upstream with a service: %v", err)
	}
	if err := c.Validate("", Site{Name: "w", Kind: Proxy, Upstream: AutoUpstream}); err == nil || !strings.Contains(err.Error(), "needs a service") {
		t.Errorf("auto upstream without a service: %v", err)
	}
}

func TestNormalizeUpstream(t *testing.T) {
	if got := NormalizeUpstream(" 8096 "); got != "localhost:8096" {
		t.Errorf("got %q", got)
	}
	if got := NormalizeUpstream("nas:80"); got != "nas:80" {
		t.Errorf("got %q", got)
	}
}
