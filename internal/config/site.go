package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Kind string

const (
	Proxy  Kind = "proxy"  // reverse_proxy to Upstream
	Static Kind = "static" // file_server for Root
)

// Site is one address Caddy serves.
type Site struct {
	// Name is the short name ("jellyfin") or a full address with a dot.
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Kind    Kind   `json:"kind"`
	// Upstream is host:port (or a bare port, meaning localhost) for a
	// proxy site; an http:// or https:// prefix is kept.
	Upstream string `json:"upstream,omitempty"`
	// Root is the directory a static site serves.
	Root string `json:"root,omitempty"`
	// HTTPS serves the site with a certificate from Caddy's internal CA;
	// off serves plain HTTP.
	HTTPS bool `json:"https"`
	// TrustedOnly imports the main Caddyfile's trusted snippet.
	TrustedOnly bool `json:"trusted_only"`
	// RedirectRoot, when set, redirects requests for exactly "/" there.
	RedirectRoot string `json:"redirect_root,omitempty"`
	// Service is the systemd unit running the app behind the site, and
	// ServiceScope its manager ("user" or "system"); both empty for none.
	Service      string `json:"service,omitempty"`
	ServiceScope string `json:"service_scope,omitempty"`
}

// AutoUpstream is the Upstream of a site that follows its service's port:
// the service answers caddyman-info, and its port is looked up whenever the
// site's file is written.
const AutoUpstream = "auto"

// IsAuto reports whether s is a proxy whose Upstream is AutoUpstream.
func (s Site) IsAuto() bool {
	return s.Kind == Proxy && strings.TrimSpace(s.Upstream) == AutoUpstream
}

// ServiceKey identifies the site's unit, "<scope>/<unit>" like ServiceMan's
// keys ("" when the site has none).
func (s Site) ServiceKey() string {
	if s.Service == "" {
		return ""
	}
	return s.ServiceScope + "/" + s.Service
}

// Address is the name the site is served under (see Config.Address).
func (s Site) Address(suffix string) string {
	n := strings.ToLower(strings.TrimSpace(s.Name))
	if n == "" || strings.Contains(n, ".") || suffix == "" {
		return n
	}
	return n + "." + strings.Trim(suffix, ".")
}

// URL is the address with its scheme, e.g. https://jellyfin.home.arpa.
func (s Site) URL(suffix string) string {
	if s.HTTPS {
		return "https://" + s.Address(suffix)
	}
	return "http://" + s.Address(suffix)
}

// Target is what the site serves, for lists: the upstream or the directory.
func (s Site) Target() string {
	if s.Kind == Static {
		return s.Root
	}
	return NormalizeUpstream(s.Upstream)
}

// NormalizeUpstream turns a bare port into localhost:<port>.
func NormalizeUpstream(u string) string {
	u = strings.TrimSpace(u)
	if _, err := strconv.Atoi(u); err == nil {
		return "localhost:" + u
	}
	return u
}

var label = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// ValidName checks a site name or full address: dot-separated DNS labels.
func ValidName(n string) error {
	n = strings.ToLower(strings.TrimSpace(n))
	if n == "" {
		return fmt.Errorf("Name is required")
	}
	if len(n) > 253 {
		return fmt.Errorf("Name is too long")
	}
	for _, l := range strings.Split(n, ".") {
		if len(l) > 63 || !label.MatchString(l) {
			return fmt.Errorf("Name %q: use letters, digits and dashes, with dots between the parts", n)
		}
	}
	return nil
}

// ValidUpstream checks a proxy target: [http(s)://]host:port or a port.
func ValidUpstream(u string) error {
	u = NormalizeUpstream(u)
	if u == "" {
		return fmt.Errorf("Upstream is required (a port, or host:port)")
	}
	hp := u
	if i := strings.Index(u, "://"); i >= 0 {
		if s := u[:i]; s != "http" && s != "https" {
			return fmt.Errorf("Upstream %q: only http:// or https:// can go before the address", u)
		}
		hp = u[i+3:]
	}
	host, port, err := net.SplitHostPort(hp)
	if err != nil || host == "" || strings.ContainsAny(host, "/ ") {
		return fmt.Errorf("Upstream %q: write it as host:port, e.g. localhost:8096, or just the port", u)
	}
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		return fmt.Errorf("Upstream %q: the port must be a number from 1 to 65535", u)
	}
	return nil
}

// ValidRoot checks a static site's directory: an existing directory.
func ValidRoot(root string) error {
	if root == "" {
		return fmt.Errorf("Directory is required")
	}
	p := ExpandTilde(root)
	if !filepath.IsAbs(p) {
		return fmt.Errorf("Directory %q: use a full path (or one starting with ~/)", root)
	}
	fi, err := os.Stat(p)
	if err != nil {
		return fmt.Errorf("Directory %s: %v", root, unwrapPathErr(err))
	}
	if !fi.IsDir() {
		return fmt.Errorf("Directory %s is a file, not a directory", root)
	}
	return nil
}

func unwrapPathErr(err error) error {
	if pe, ok := err.(*os.PathError); ok {
		return pe.Err
	}
	return err
}

// ValidRedirect checks "Redirect / to": empty, or a path or URL without
// spaces.
func ValidRedirect(r string) error {
	if r == "" {
		return nil
	}
	if strings.ContainsAny(r, " \t\"") {
		return fmt.Errorf("Redirect %q can't contain spaces or quotes", r)
	}
	if !strings.HasPrefix(r, "/") && !strings.Contains(r, "://") {
		return fmt.Errorf("Redirect %q: start it with / (e.g. /web/) or give a full URL", r)
	}
	if r == "/" {
		return fmt.Errorf("Redirecting / to / would loop")
	}
	return nil
}

// Validate checks s as it would be saved in place of the site served at
// orig ("" for a new site): its fields, and that no other site has its
// address.
func (c *Config) Validate(orig string, s Site) error {
	if err := ValidName(s.Name); err != nil {
		return err
	}
	switch s.Kind {
	case Proxy:
		if s.IsAuto() {
			if s.Service == "" {
				return fmt.Errorf("Upstream auto needs a service (an autoconfigurable one) to take the port from")
			}
		} else if err := ValidUpstream(s.Upstream); err != nil {
			return err
		}
	case Static:
		if err := ValidRoot(s.Root); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown kind %q (proxy or static)", s.Kind)
	}
	if err := ValidRedirect(s.RedirectRoot); err != nil {
		return err
	}
	if s.Service != "" && s.ServiceScope != "user" && s.ServiceScope != "system" {
		return fmt.Errorf("service %s: scope must be user or system, not %q", s.Service, s.ServiceScope)
	}
	addr := c.Address(s)
	if addr != orig && c.Index(addr) >= 0 {
		return fmt.Errorf("%s already exists", addr)
	}
	return nil
}
