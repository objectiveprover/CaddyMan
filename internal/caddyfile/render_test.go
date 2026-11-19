package caddyfile

import (
	"strings"
	"testing"

	"caddyman/internal/config"
)

func testConfig() *config.Config {
	return &config.Config{DomainSuffix: "home.arpa", TrustedSnippet: "trusted_only"}
}

func TestSite(t *testing.T) {
	c := testConfig()
	for _, tc := range []struct {
		site config.Site
		want string
	}{
		{config.Site{Name: "jellyfin", Kind: config.Proxy, Upstream: "8096", HTTPS: true, TrustedOnly: true, RedirectRoot: "/web/"}, `jellyfin.home.arpa {
	tls internal
	import trusted_only
	redir / /web/
	reverse_proxy localhost:8096
}
`},
		{config.Site{Name: "docs", Kind: config.Static, Root: "/srv/my docs"}, `http://docs.home.arpa {
	root * "/srv/my docs"
	file_server
}
`},
		{config.Site{Name: "nas.lan", Kind: config.Proxy, Upstream: "https://192.168.1.2:5001", HTTPS: true}, `nas.lan {
	tls internal
	reverse_proxy https://192.168.1.2:5001
}
`},
	} {
		if got := Site(c, tc.site); got != tc.want {
			t.Errorf("Site(%s):\n%s\nwant:\n%s", tc.site.Name, got, tc.want)
		}
	}
}

func TestFileStartsWithMarker(t *testing.T) {
	got := File(testConfig(), config.Site{Name: "a", Kind: config.Proxy, Upstream: "1"})
	if !strings.HasPrefix(got, Marker) || !strings.Contains(got, "http://a.home.arpa {") {
		t.Errorf("File:\n%s", got)
	}
}

func TestSitePreviewPlaceholders(t *testing.T) {
	got := Site(testConfig(), config.Site{Kind: config.Proxy, HTTPS: true})
	if !strings.HasPrefix(got, "name.home.arpa {") || !strings.Contains(got, "reverse_proxy localhost:PORT") {
		t.Errorf("empty site preview:\n%s", got)
	}
}

func TestQuote(t *testing.T) {
	for in, want := range map[string]string{
		"/web/":   "/web/",
		"a b":     `"a b"`,
		`a"b`:     `"a\"b"`,
		`{x}`:     `"{x}"`,
		"":        `""`,
		"a#b":     `"a#b"`,
		`c:\path`: `"c:\\path"`,
	} {
		if got := Quote(in); got != want {
			t.Errorf("Quote(%q) = %s, want %s", in, got, want)
		}
	}
}
