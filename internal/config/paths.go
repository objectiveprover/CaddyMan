package config

import (
	"os"
	"path/filepath"
	"strings"
)

// Where CaddyMan keeps its files (XDG directories), and "~" helpers.

func xdg(env, fallback string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, fallback)
}

// ConfigHome is $XDG_CONFIG_HOME, or ~/.config.
func ConfigHome() string { return xdg("XDG_CONFIG_HOME", ".config") }

func Dir() string { return filepath.Join(ConfigHome(), "caddyman") }

func Path() string { return filepath.Join(Dir(), "config.json") }

// SitesDir holds CaddyMan's copy of the site files, one <address>.caddy per
// enabled site, generated from the config. Apply changes copies them to
// caddy_sites_dir.
func SitesDir() string { return filepath.Join(Dir(), "sites") }

// Tilde abbreviates a path under the home directory with "~".
func Tilde(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || p == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+"/") {
		return "~" + p[len(home):]
	}
	return p
}

// ExpandTilde turns a leading "~/" into the home directory.
func ExpandTilde(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + p[1:]
		}
	}
	return p
}
