// Package config holds CaddyMan's settings and sites (config.go, site.go),
// where its files live (paths.go), and `caddyman config update` (merge.go).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// CurrentVersion is the config format version this build expects. Bump it
// whenever a setting is added or removed (and in config.template.json).
const CurrentVersion = 1

// Defaults for settings that are missing or empty. config.template.json
// carries the same values (a test checks they match).
const (
	DefaultDomainSuffix     = "home.arpa"
	DefaultCaddyBin         = "caddy"
	DefaultCaddyfile        = "/etc/caddy/Caddyfile"
	DefaultCaddySitesDir    = "/etc/caddy/caddyman"
	DefaultTrustedSnippet   = "trusted_only"
	DefaultRootCert         = "/var/lib/caddy/.local/share/caddy/pki/authorities/local/root.crt"
	DefaultServicemanConfig = "~/.config/serviceman/config.json"
)

type Config struct {
	// Version is the config format version the file was written for;
	// `caddyman config version` compares it with CurrentVersion.
	Version int `json:"version"`
	// DomainSuffix is added to site names without a dot: "jellyfin" is
	// served as jellyfin.<suffix>.
	DomainSuffix string `json:"domain_suffix"`
	// CaddyBin is the caddy command used to check the config (a name in
	// PATH or a path).
	CaddyBin string `json:"caddy_bin"`
	// Caddyfile is Caddy's main Caddyfile, which imports CaddySitesDir.
	Caddyfile string `json:"caddyfile"`
	// CaddySitesDir is where Apply changes copies the site files (as root)
	// for Caddy to read; CaddyMan's own copies are in SitesDir().
	CaddySitesDir string `json:"caddy_sites_dir"`
	// TrustedSnippet is the snippet, defined in the main Caddyfile, that a
	// "Trusted only" site imports.
	TrustedSnippet string `json:"trusted_snippet"`
	// RootCert is the root certificate of Caddy's internal CA.
	RootCert string `json:"root_cert"`
	// ServicemanConfig is ServiceMan's config, whose tracked services the
	// site form offers.
	ServicemanConfig string `json:"serviceman_config"`
	// LanIP is this machine's address in the hosts lines; empty finds it.
	LanIP string `json:"lan_ip"`
	Sites []Site `json:"sites"`
}

// Load reads the config, returning defaults if it does not exist yet.
func Load() (*Config, error) {
	c := &Config{}
	b, err := os.ReadFile(Path())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if err := json.Unmarshal(b, c); err != nil {
			return nil, fmt.Errorf("parse %s: %w", Tilde(Path()), err)
		}
	} else {
		c.Version = CurrentVersion // a new file starts at the current format
	}
	c.applyDefaults()
	return c, nil
}

// applyDefaults fills in missing settings. Paths keep their "~" so saving
// writes them back as the user wrote them; the *Path methods expand them.
func (c *Config) applyDefaults() {
	set := func(p *string, d string) {
		if *p == "" {
			*p = d
		}
	}
	set(&c.DomainSuffix, DefaultDomainSuffix)
	set(&c.CaddyBin, DefaultCaddyBin)
	set(&c.Caddyfile, DefaultCaddyfile)
	set(&c.CaddySitesDir, DefaultCaddySitesDir)
	set(&c.TrustedSnippet, DefaultTrustedSnippet)
	set(&c.RootCert, DefaultRootCert)
	set(&c.ServicemanConfig, DefaultServicemanConfig)
	if c.Sites == nil {
		c.Sites = []Site{}
	}
	for i := range c.Sites {
		if c.Sites[i].Kind == "" {
			c.Sites[i].Kind = Proxy
		}
	}
}

func (c *Config) CaddyBinPath() string         { return ExpandTilde(c.CaddyBin) }
func (c *Config) CaddyfilePath() string        { return ExpandTilde(c.Caddyfile) }
func (c *Config) CaddySitesDirPath() string    { return ExpandTilde(c.CaddySitesDir) }
func (c *Config) RootCertPath() string         { return ExpandTilde(c.RootCert) }
func (c *Config) ServicemanConfigPath() string { return ExpandTilde(c.ServicemanConfig) }

// FileName is the name of a site's file: <address>.caddy.
func (c *Config) FileName(s Site) string { return c.Address(s) + ".caddy" }

// LocalFile is CaddyMan's copy of a site's file (the one it edits).
func (c *Config) LocalFile(s Site) string { return filepath.Join(SitesDir(), c.FileName(s)) }

// CaddyFile is the copy Caddy reads, written by Apply changes.
func (c *Config) CaddyFile(s Site) string {
	return filepath.Join(c.CaddySitesDirPath(), c.FileName(s))
}

// Address is the name a site is served under: its name, plus the domain
// suffix when the name has no dot.
func (c *Config) Address(s Site) string { return s.Address(c.DomainSuffix) }

// Index returns the position of the site served at addr, or -1.
func (c *Config) Index(addr string) int {
	for i, s := range c.Sites {
		if c.Address(s) == addr {
			return i
		}
	}
	return -1
}

// Find returns the site served at addr, or nil.
func (c *Config) Find(addr string) *Site {
	if i := c.Index(addr); i >= 0 {
		return &c.Sites[i]
	}
	return nil
}

// Put replaces the site served at orig (empty for a new site) with s, or
// adds s when there is none.
func (c *Config) Put(orig string, s Site) {
	if i := c.Index(orig); orig != "" && i >= 0 {
		c.Sites[i] = s
		return
	}
	c.Sites = append(c.Sites, s)
}

// Remove deletes the site served at addr and reports whether there was one.
func (c *Config) Remove(addr string) bool {
	i := c.Index(addr)
	if i < 0 {
		return false
	}
	c.Sites = append(c.Sites[:i], c.Sites[i+1:]...)
	return true
}

// Sorted returns the sites ordered by address, the order of the list.
func (c *Config) Sorted() []Site {
	out := append([]Site(nil), c.Sites...)
	sort.SliceStable(out, func(i, j int) bool { return c.Address(out[i]) < c.Address(out[j]) })
	return out
}

// Save writes the config atomically.
func (c *Config) Save() error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := Path() + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, Path())
}
