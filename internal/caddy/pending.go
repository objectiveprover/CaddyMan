package caddy

import (
	"bytes"
	"sort"
	"strings"

	"caddyman/internal/config"
)

// PendingSet is what Apply changes would change in caddy_sites_dir, by site
// address.
type PendingSet struct {
	Added, Changed, Removed []string
}

// Count is the number of sites that would change.
func (p PendingSet) Count() int { return len(p.Added) + len(p.Changed) + len(p.Removed) }

// Has reports whether the site served at addr would change.
func (p PendingSet) Has(addr string) bool {
	for _, l := range [][]string{p.Added, p.Changed, p.Removed} {
		for _, a := range l {
			if a == addr {
				return true
			}
		}
	}
	return false
}

// Pending compares the sites' files as the config says (Desired) with
// CaddyMan's files in caddy_sites_dir, which anyone can read.
func Pending(c *config.Config) (PendingSet, error) {
	have, _, err := ownedFiles(c.CaddySitesDirPath())
	if err != nil {
		return PendingSet{}, err
	}
	var p PendingSet
	addr := func(name string) string { return strings.TrimSuffix(name, ".caddy") }
	for name, b := range Desired(c) {
		cur, ok := have[name]
		switch {
		case !ok:
			p.Added = append(p.Added, addr(name))
		case !bytes.Equal(cur, b):
			p.Changed = append(p.Changed, addr(name))
		}
	}
	want := Desired(c)
	for name := range have {
		if _, ok := want[name]; !ok {
			p.Removed = append(p.Removed, addr(name))
		}
	}
	sort.Strings(p.Added)
	sort.Strings(p.Changed)
	sort.Strings(p.Removed)
	return p, nil
}
