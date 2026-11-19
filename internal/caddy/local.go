package caddy

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"caddyman/internal/caddyfile"
	"caddyman/internal/config"
)

// Saved says how a local save went.
type Saved struct {
	Checked   bool   // Caddy accepted the sites with your Caddyfile
	Unchecked string // why it couldn't be checked ("" when Checked)
}

// Note is a short summary for messages.
func (s Saved) Note() string {
	if s.Checked {
		return "apply with C-x C-s"
	}
	return "not checked (" + s.Unchecked + "); apply with C-x C-s"
}

// Desired is every enabled site's file, by file name, as the config says,
// with auto upstreams resolved (see Resolve).
func Desired(c *config.Config) (map[string][]byte, error) {
	r, err := Resolve(c)
	if err != nil {
		return nil, err
	}
	want := map[string][]byte{}
	for _, s := range r.Sites {
		if s.Enabled {
			want[r.FileName(s)] = []byte(caddyfile.File(r, s))
		}
	}
	return want, nil
}

// ownedFiles reads the site files CaddyMan wrote: *.caddy files in dir
// that start with caddyfile.Marker, by file name. A missing dir has none.
func ownedFiles(dir string) (owned map[string][]byte, foreign map[string]bool, err error) {
	owned, foreign = map[string][]byte{}, map[string]bool{}
	paths, err := filepath.Glob(filepath.Join(dir, "*.caddy"))
	if err != nil {
		return nil, nil, err
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, nil, err
		}
		if bytes.HasPrefix(b, []byte(caddyfile.Marker)) {
			owned[filepath.Base(p)] = b
		} else {
			foreign[filepath.Base(p)] = true
		}
	}
	return owned, foreign, nil
}

// writeFile replaces path atomically. The temporary name starts with a dot
// and doesn't end in .caddy, so an import glob never picks it up.
func writeFile(path string, data []byte) error {
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// sync makes dir hold exactly want (file name → content) among CaddyMan's
// files, and returns how to put the old ones back.
func sync(dir string, want map[string][]byte) (restore func(), err error) {
	old, _, err := ownedFiles(dir)
	if err != nil {
		return nil, err
	}
	restore = func() {
		for name := range want {
			if _, had := old[name]; !had {
				os.Remove(filepath.Join(dir, name))
			}
		}
		for name, b := range old {
			writeFile(filepath.Join(dir, name), b)
		}
	}
	for name, b := range want {
		if bytes.Equal(old[name], b) {
			continue
		}
		if err := writeFile(filepath.Join(dir, name), b); err != nil {
			restore()
			return nil, err
		}
	}
	for name := range old {
		if _, keep := want[name]; !keep {
			if err := os.Remove(filepath.Join(dir, name)); err != nil {
				restore()
				return nil, err
			}
		}
	}
	return restore, nil
}

// SaveLocal writes the sites' files into CaddyMan's own directory
// (~/.config/caddyman/sites) and checks them with `caddy adapt` together
// with your main Caddyfile (see checkFile). If Caddy rejects them, the old
// files are put back and save isn't run. Nothing reaches Caddy until Apply
// changes copies the files.
func SaveLocal(c *config.Config, save func() error) (Saved, error) {
	dir := config.SitesDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Saved{}, err
	}
	// A hand-written file in Caddy's directory with a site's name would
	// stop Apply changes: say so now.
	_, foreign, _ := ownedFiles(c.CaddySitesDirPath())
	for _, s := range c.Sites {
		if s.Enabled && foreign[c.FileName(s)] {
			return Saved{}, fmt.Errorf("%s wasn't written by CaddyMan, so Apply changes would stop at it; rename the site or move that file",
				c.CaddyFile(s))
		}
	}
	want, err := Desired(c)
	if err != nil {
		return Saved{}, err
	}
	restore, err := sync(dir, want)
	if err != nil {
		return Saved{}, err
	}
	saved := Saved{}
	if err := checkSites(c); errors.Is(err, errUnchecked) {
		saved.Unchecked = strings.TrimPrefix(err.Error(), errUnchecked.Error()+": ")
	} else if err != nil {
		restore()
		return Saved{}, fmt.Errorf("Caddy rejected the change: %w", err)
	} else {
		saved.Checked = true
	}
	if save != nil {
		if err := save(); err != nil {
			restore()
			return Saved{}, err
		}
	}
	return saved, nil
}

// Update loads the config, changes it with fn and saves it locally (see
// SaveLocal), keeping the config only when Caddy accepts the result.
func Update(fn func(*config.Config) error) (*config.Config, Saved, error) {
	c, err := config.Load()
	if err != nil {
		return nil, Saved{}, err
	}
	if err := fn(c); err != nil {
		return nil, Saved{}, err
	}
	s, err := SaveLocal(c, c.Save)
	if err != nil {
		return nil, s, err
	}
	return c, s, nil
}

var errUnchecked = errors.New("unchecked")

// checkSites runs `caddy adapt` on checkFile's Caddyfile. It returns
// errUnchecked (wrapped, with the reason) when it can't check.
func checkSites(c *config.Config) error {
	bin, err := Find(c.CaddyBinPath())
	if err != nil {
		return fmt.Errorf("%w: caddy isn't installed", errUnchecked)
	}
	path, err := checkFile(c)
	if err != nil {
		return fmt.Errorf("%w: %v", errUnchecked, err)
	}
	if _, err := Adapt(bin, path); err != nil {
		if strings.Contains(err.Error(), "permission denied") {
			return fmt.Errorf("%w: %v", errUnchecked, err)
		}
		return err
	}
	return nil
}

var reImportLine = regexp.MustCompile(`^(\s*import\s+)(\S+)(.*)$`)
var reSnippetDef = regexp.MustCompile(`^\s*\(([^)\s]+)\)`)

// checkFile writes ~/.config/caddyman/.check/Caddyfile: your main Caddyfile
// with its import of caddy_sites_dir taken out, its other relative imports
// made absolute (they're relative to the main Caddyfile's directory), and
// an import of CaddyMan's own site files added at the end, after your
// snippets. Adapting it checks the sites as Caddy would see them after
// Apply changes, without root.
func checkFile(c *config.Config) (string, error) {
	main := c.CaddyfilePath()
	b, err := os.ReadFile(main)
	if err != nil {
		return "", fmt.Errorf("can't read %s", main)
	}
	snippets := map[string]bool{}
	for _, l := range strings.Split(string(b), "\n") {
		if m := reSnippetDef.FindStringSubmatch(l); m != nil {
			snippets[m[1]] = true
		}
	}
	probe := filepath.Join(c.CaddySitesDirPath(), "example.home.arpa.caddy")
	var out strings.Builder
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := sc.Text()
		if m := reImportLine.FindStringSubmatch(line); m != nil && !snippets[strings.Trim(m[2], `"`)] {
			arg := strings.Trim(m[2], `"`)
			abs := arg
			if !filepath.IsAbs(arg) {
				abs = filepath.Join(filepath.Dir(main), arg)
			}
			if ok, _ := filepath.Match(abs, probe); ok {
				continue // CaddyMan's sites: replaced by the local ones below
			}
			line = m[1] + caddyfile.Quote(abs) + m[3]
		}
		out.WriteString(line + "\n")
	}
	out.WriteString("\n# CaddyMan's sites, before they're applied\nimport " +
		caddyfile.Quote(filepath.Join(config.SitesDir(), "*.caddy")) + "\n")
	dir := filepath.Join(config.Dir(), ".check")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "Caddyfile")
	return path, os.WriteFile(path, []byte(out.String()), 0o600)
}
