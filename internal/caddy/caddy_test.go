package caddy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"caddyman/internal/config"
)

func TestCaddyError(t *testing.T) {
	out := `{"level":"info","msg":"using config from file"}
Error: adapting config using caddyfile: /x/Caddyfile:12: unrecognized directive: bogus`
	if got := caddyError(out); got != "adapting config using caddyfile: /x/Caddyfile:12: unrecognized directive: bogus" {
		t.Errorf("caddyError = %q", got)
	}
	long := "Error: loading http app module: provision http: server srv0: setting up route handlers: invalid IP address: 'x': ParseAddr(\"x\"): unable to parse IP"
	if got := caddyError(long); got != `…: invalid IP address: 'x': ParseAddr("x"): unable to parse IP` {
		t.Errorf("caddyError(long) = %q", got)
	}
	reload := `{"level":"error","ts":1,"error":"sending configuration to instance: caddy responded with error: HTTP 400: {\"error\":\"loading config: loading new config: http app module: start: listening on :443: listen tcp :443: bind: permission denied\"}\n"}`
	if got := caddyError(reload); got != "…: listening on :443: listen tcp :443: bind: permission denied" {
		t.Errorf("caddyError(reload) = %q", got)
	}
	adapt := `{"level":"warn","msg":"No files matching import glob pattern"}
{"level":"error","ts":1,"msg":"File to import not found: trusted_only, at /etc/caddy/caddyman/a.caddy:2"}`
	if got := caddyError(adapt); got != "File to import not found: trusted_only, at /etc/caddy/caddyman/a.caddy:2" {
		t.Errorf("caddyError(adapt) = %q", got)
	}
}

// fakeBin puts fake caddy and systemctl commands first in PATH and returns
// the caddy one. caddy's adapt follows the config's absolute and relative
// file imports and prints the site addresses it finds as JSON strings (like
// the real config's host matchers); it fails when $FAKE_CADDY_FAIL is set.
// systemctl says caddy is active when $FAKE_ACTIVE is set, fails reloads
// when $FAKE_RELOAD_FAIL is set, and logs its calls to $FAKE_LOG.
func fakeBin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	caddy := `#!/bin/sh
case "$1" in
version) echo "v2.99.0 h1:x" ;;
adapt)
  if [ -n "${FAKE_CADDY_FAIL:-}" ]; then echo '{"level":"error","msg":"'"$FAKE_CADDY_FAIL"'"}' >&2; exit 1; fi
  cfg=$3
  sed -n 's/^[[:space:]]*import[[:space:]]\{1,\}"\{0,1\}\([^" ]*\)"\{0,1\}.*/\1/p' "$cfg" | while read -r pat; do
    case "$pat" in /*) ;; *) pat="$(dirname "$cfg")/$pat" ;; esac
    for f in $pat; do [ -f "$f" ] && sed -n 's|^\(http://\)\{0,1\}\([a-z0-9.-]*\) {$|"\2"|p' "$f"; done
  done; true ;;
esac
`
	systemctl := `#!/bin/sh
echo "$*" >> "${FAKE_LOG:-/dev/null}"
case "$1" in
is-active) [ -n "${FAKE_ACTIVE:-}" ] ;;
reload) [ -z "${FAKE_RELOAD_FAIL:-}" ] ;;
esac
`
	os.WriteFile(filepath.Join(dir, "caddy"), []byte(caddy), 0o755)
	os.WriteFile(filepath.Join(dir, "systemctl"), []byte(systemctl), 0o755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return filepath.Join(dir, "caddy")
}

// testConfig is a CaddyMan config whose Caddy lives in a temp "/etc/caddy":
// a main Caddyfile defining trusted_only and importing caddyman/*.caddy.
func testConfig(t *testing.T) *config.Config {
	t.Helper()
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	etc := filepath.Join(t.TempDir(), "caddy")
	os.MkdirAll(filepath.Join(etc, "snippets"), 0o755)
	os.WriteFile(filepath.Join(etc, "Caddyfile"), []byte(
		"{\n}\n(trusted_only) {\n\tabort\n}\nimport snippets/*.caddy\nimport trusted_only\nimport caddyman/*.caddy\n"), 0o644)
	c, _ := config.Load()
	c.Caddyfile = filepath.Join(etc, "Caddyfile")
	c.CaddySitesDir = filepath.Join(etc, "caddyman")
	c.CaddyBin = fakeBin(t)
	t.Setenv("CADDYMAN_SUDO", "env") // run the apply script without root
	t.Setenv("FAKE_CADDY_FAIL", "")
	t.Setenv("FAKE_ACTIVE", "")
	t.Setenv("FAKE_RELOAD_FAIL", "")
	t.Setenv("FAKE_LOG", filepath.Join(t.TempDir(), "systemctl.log"))
	return c
}

func list(dir string) []string {
	var out []string
	es, _ := os.ReadDir(dir)
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

func TestCheckFile(t *testing.T) {
	c := testConfig(t)
	path, err := checkFile(c)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	got := string(b)
	etc := filepath.Dir(c.CaddyfilePath())
	for _, want := range []string{
		"import " + filepath.Join(etc, "snippets/*.caddy") + "\n", // relative import made absolute
		"import trusted_only\n",                                   // snippet import untouched
		"import " + filepath.Join(config.SitesDir(), "*.caddy") + "\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("check Caddyfile lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "caddyman/*.caddy") && !strings.Contains(got, config.SitesDir()) {
		t.Errorf("Caddy's import of CaddyMan's sites wasn't replaced:\n%s", got)
	}
	if strings.Count(got, "import ") != 3 {
		t.Errorf("want 3 imports (Caddy's sites import dropped):\n%s", got)
	}
}

func TestSaveLocalAndPending(t *testing.T) {
	c := testConfig(t)
	c.Put("", config.Site{Name: "a", Enabled: true, Kind: config.Proxy, Upstream: "80", HTTPS: true})
	c.Put("", config.Site{Name: "b", Enabled: true, Kind: config.Proxy, Upstream: "81"})
	c.Put("", config.Site{Name: "off", Enabled: false, Kind: config.Proxy, Upstream: "82"})
	saved := 0
	s, err := SaveLocal(c, func() error { saved++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if !s.Checked || saved != 1 {
		t.Errorf("SaveLocal = %+v (saved %d)", s, saved)
	}
	if got := strings.Join(list(config.SitesDir()), " "); got != "a.home.arpa.caddy b.home.arpa.caddy" {
		t.Errorf("local files: %s", got)
	}
	if _, err := os.Stat(c.CaddySitesDirPath()); err == nil {
		t.Error("a local save touched Caddy's directory")
	}
	p, _ := Pending(c)
	if strings.Join(p.Added, " ") != "a.home.arpa b.home.arpa" || p.Count() != 2 || !p.Has("a.home.arpa") || p.Has("off.home.arpa") {
		t.Errorf("Pending = %+v", p)
	}

	// Rejected: the local files go back and the config isn't saved.
	t.Setenv("FAKE_CADDY_FAIL", "bad site")
	c.Find("a.home.arpa").Upstream = "localhost:99"
	saved = 0
	if _, err := SaveLocal(c, func() error { saved++; return nil }); err == nil || !strings.Contains(err.Error(), "bad site") {
		t.Fatalf("SaveLocal = %v, want Caddy's error", err)
	}
	b, _ := os.ReadFile(c.LocalFile(*c.Find("a.home.arpa")))
	if saved != 0 || !strings.Contains(string(b), "localhost:80") {
		t.Errorf("not rolled back (saved %d):\n%s", saved, b)
	}

	// No main Caddyfile: saved, but not checked.
	t.Setenv("FAKE_CADDY_FAIL", "")
	c.Caddyfile = filepath.Join(t.TempDir(), "none")
	if s, err := SaveLocal(c, nil); err != nil || s.Checked || !strings.Contains(s.Note(), "not checked") {
		t.Errorf("SaveLocal without a Caddyfile = %+v, %v", s, err)
	}
}

func TestDeploy(t *testing.T) {
	c := testConfig(t)
	c.Put("", config.Site{Name: "a", Enabled: true, Kind: config.Proxy, Upstream: "80"})
	c.Put("", config.Site{Name: "b", Enabled: true, Kind: config.Proxy, Upstream: "81"})
	if _, err := SaveLocal(c, nil); err != nil {
		t.Fatal(err)
	}
	dst := c.CaddySitesDirPath()
	// A hand-written file in Caddy's directory is never touched.
	os.MkdirAll(dst, 0o755)
	os.WriteFile(filepath.Join(dst, "mine.caddy"), []byte("mine.home.arpa {\n}\n"), 0o644)
	t.Setenv("FAKE_ACTIVE", "1")

	out, needAuth, err := DeployQuick(c)
	if err != nil || needAuth {
		t.Fatalf("DeployQuick = %v (needAuth %v):\n%s", err, needAuth, out)
	}
	if got := strings.Join(list(dst), " "); got != "a.home.arpa.caddy b.home.arpa.caddy mine.caddy" {
		t.Errorf("Caddy's files: %s", got)
	}
	if log, _ := os.ReadFile(os.Getenv("FAKE_LOG")); !strings.Contains(string(log), "reload caddy") {
		t.Errorf("Caddy wasn't reloaded; systemctl calls:\n%s", log)
	}
	if p, _ := Pending(c); p.Count() != 0 {
		t.Errorf("pending after apply: %+v", p)
	}
	if m := Missing(c); len(m) != 0 {
		t.Errorf("Missing = %v", m)
	}

	// Delete b, change a; the reload fails: everything goes back.
	c.Remove("b.home.arpa")
	c.Find("a.home.arpa").Upstream = "localhost:99"
	SaveLocal(c, nil)
	if p, _ := Pending(c); strings.Join(p.Removed, " ") != "b.home.arpa" || strings.Join(p.Changed, " ") != "a.home.arpa" {
		t.Errorf("Pending = %+v", p)
	}
	t.Setenv("FAKE_RELOAD_FAIL", "1")
	if out, _, err := DeployQuick(c); err == nil || !strings.Contains(out, "putting the previous site files back") {
		t.Fatalf("DeployQuick with a failing reload = %v:\n%s", err, out)
	}
	if got := strings.Join(list(dst), " "); got != "a.home.arpa.caddy b.home.arpa.caddy mine.caddy" {
		t.Errorf("after a failed apply: %s", got)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "a.home.arpa.caddy")); !strings.Contains(string(b), "localhost:80") {
		t.Errorf("a wasn't put back:\n%s", b)
	}

	// Rejected by caddy adapt: same.
	t.Setenv("FAKE_RELOAD_FAIL", "")
	t.Setenv("FAKE_CADDY_FAIL", "bad")
	if _, _, err := DeployQuick(c); err == nil {
		t.Fatal("an apply Caddy rejects succeeded")
	}
	if got := strings.Join(list(dst), " "); got != "a.home.arpa.caddy b.home.arpa.caddy mine.caddy" {
		t.Errorf("after a rejected apply: %s", got)
	}

	// Now it works: b's file is removed, a's updated, mine kept.
	t.Setenv("FAKE_CADDY_FAIL", "")
	if out, _, err := DeployQuick(c); err != nil {
		t.Fatalf("DeployQuick = %v:\n%s", err, out)
	}
	if got := strings.Join(list(dst), " "); got != "a.home.arpa.caddy mine.caddy" {
		t.Errorf("after deleting b: %s", got)
	}

	// A site named like the hand-written file is refused before anything is copied.
	c.Put("", config.Site{Name: "mine", Enabled: true, Kind: config.Proxy, Upstream: "1"})
	os.Rename(filepath.Join(dst, "mine.caddy"), filepath.Join(dst, "mine.home.arpa.caddy"))
	if _, err := SaveLocal(c, nil); err == nil || !strings.Contains(err.Error(), "wasn't written by CaddyMan") {
		t.Errorf("SaveLocal over a hand-written file: %v", err)
	}
}

func TestFindHookup(t *testing.T) {
	dir := t.TempDir()
	cf := filepath.Join(dir, "Caddyfile")
	sites := filepath.Join(dir, "caddyman")
	for _, tc := range []struct {
		text      string
		imp, snip int
	}{
		{"{\n}\n(trusted_only) {\n\tabort\n}\nimport caddyman/*.caddy\n", 6, 3},
		{"import " + sites + "/*.caddy\n(trusted_only) {\n}\n", 1, 2},
		{"import \"caddyman/*\"\n", 1, 0},
		{"import other/*.caddy\n", 0, 0},
	} {
		os.WriteFile(cf, []byte(tc.text), 0o644)
		h, err := FindHookup(cf, sites, "trusted_only")
		if err != nil || h.ImportLine != tc.imp || h.SnippetLine != tc.snip {
			t.Errorf("FindHookup(%q) = %+v, %v; want import %d, snippet %d", tc.text, h, err, tc.imp, tc.snip)
		}
	}
}
