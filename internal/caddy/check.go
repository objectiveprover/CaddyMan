package caddy

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"caddyman/internal/config"
)

// Check is one line of `caddyman check`.
type Check struct {
	OK   bool
	Text string
	Fix  string // what to do when !OK
}

var reImport = regexp.MustCompile(`^\s*import\s+(\S+)`)

// Hookup is what the main Caddyfile says about CaddyMan's sites: the line
// importing the sites directory and the line defining the trusted snippet
// (0 when missing).
type Hookup struct {
	ImportLine, SnippetLine int
}

// FindHookup reads the main Caddyfile for an `import` that matches files in
// sitesDir and for the definition of snippet.
func FindHookup(caddyfile, sitesDir, snippet string) (Hookup, error) {
	f, err := os.Open(caddyfile)
	if err != nil {
		return Hookup{}, err
	}
	defer f.Close()
	var h Hookup
	probe := filepath.Join(sitesDir, "example.home.arpa.caddy")
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if h.SnippetLine == 0 && strings.HasPrefix(strings.TrimSpace(line), "("+snippet+")") {
			h.SnippetLine = n
		}
		m := reImport.FindStringSubmatch(line)
		if m == nil || h.ImportLine != 0 {
			continue
		}
		pat := strings.Trim(m[1], `"`)
		if !filepath.IsAbs(pat) {
			pat = filepath.Join(filepath.Dir(caddyfile), pat)
		}
		if ok, _ := filepath.Match(pat, probe); ok {
			h.ImportLine = n
		}
	}
	return h, sc.Err()
}

// CheckSetup checks everything CaddyMan relies on and says how to fix what
// isn't right. It changes nothing.
func CheckSetup(c *config.Config) []Check {
	var out []Check
	add := func(ok bool, text, fix string) { out = append(out, Check{ok, text, fix}) }

	bin, err := Find(c.CaddyBinPath())
	if err != nil {
		add(false, err.Error(), "install Caddy as a normal system service, e.g. from Caddy's apt repository (caddyserver.com/docs/install)")
	} else {
		v, _ := Version(bin)
		add(true, fmt.Sprintf("caddy %s is installed (%s)", v, bin), "")
	}

	if st := GetStatus(c.CaddyBinPath()); st.Running {
		add(true, UnitName+" is running", "")
	} else {
		add(false, fmt.Sprintf("%s isn't running (%s)", UnitName, orDash(st.Unit)), "start it: sudo systemctl enable --now caddy")
	}

	dir := c.CaddySitesDirPath()

	cf := c.CaddyfilePath()
	rel := filepath.Base(dir) + "/*.caddy"
	if filepath.Dir(dir) != filepath.Dir(cf) {
		rel = dir + "/*.caddy"
	}
	h, err := FindHookup(cf, dir, c.TrustedSnippet)
	switch {
	case err != nil:
		add(false, "can't read "+cf+": "+err.Error(), "check caddyfile in "+config.Tilde(config.Path()))
	case h.ImportLine == 0:
		add(false, cf+" doesn't import CaddyMan's sites", "add this line to it, after your snippets: import "+rel)
	default:
		add(true, fmt.Sprintf("%s imports CaddyMan's sites (line %d)", cf, h.ImportLine), "")
	}
	if err == nil {
		used := false
		for _, s := range c.Sites {
			used = used || s.TrustedOnly
		}
		snip := "(" + c.TrustedSnippet + ")"
		switch {
		case h.SnippetLine == 0 && used:
			add(false, cf+" doesn't define the "+snip+" snippet that Trusted only sites import",
				"define it before the import line, e.g.: "+snip+" { @untrusted not remote_ip private_ranges; respond @untrusted 403 }")
		case h.SnippetLine == 0:
			add(true, cf+" doesn't define "+snip+" (only needed for Trusted only sites)", "")
		case h.ImportLine != 0 && h.SnippetLine > h.ImportLine:
			add(false, snip+" is defined after the import line, so the sites can't use it",
				fmt.Sprintf("move the import (line %d) below the snippet (line %d)", h.ImportLine, h.SnippetLine))
		default:
			add(true, fmt.Sprintf("%s defines %s (line %d)", cf, snip, h.SnippetLine), "")
		}
	}

	if p, err := Pending(c); err == nil && p.Count() > 0 {
		add(false, fmt.Sprintf("%d site changes aren't applied yet", p.Count()), "caddyman apply (or C-x C-s in CaddyMan)")
	} else if err == nil {
		add(true, "every change is applied", "")
	}

	if bin != "" {
		if _, err := Adapt(bin, cf); err != nil {
			add(false, "Caddy rejects the config: "+err.Error(), "fix "+cf+", or the site named in the error")
		} else {
			add(true, "Caddy accepts the whole config (caddy adapt)", "")
		}
	}
	return out
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
