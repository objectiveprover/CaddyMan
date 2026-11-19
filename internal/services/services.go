// Package services links sites to the systemd units behind them: the
// services ServiceMan tracks, and their current state.
package services

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Unit is a systemd service: its name, its manager ("user" or "system"),
// and the health URL ServiceMan has for it (may be empty).
type Unit struct {
	Name  string `json:"unit"`
	Scope string `json:"scope"`
	URL   string `json:"url,omitempty"`
}

// Key is "<scope>/<unit>", the same key as config.Site.ServiceKey.
func (u Unit) Key() string { return u.Scope + "/" + u.Name }

// Label is how the form shows the unit, e.g. "jellyfin.service (user)".
func (u Unit) Label() string { return u.Name + " (" + u.Scope + ")" }

// Tracked reads the services in ServiceMan's config, in its order. A
// missing config (ServiceMan not set up) is no services, not an error.
func Tracked(servicemanConfig string) ([]Unit, error) {
	b, err := os.ReadFile(servicemanConfig)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c struct {
		Services []Unit `json:"services"`
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	for i := range c.Services {
		if c.Services[i].Scope == "" {
			c.Services[i].Scope = "system" // ServiceMan's default
		}
	}
	return c.Services, nil
}

// Upstream guesses the address a service listens on from its health URL:
// http://localhost:8096/health gives localhost:8096 ("" when there's none).
func Upstream(healthURL string) string {
	u, err := url.Parse(healthURL)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	port := u.Port()
	if port == "" {
		port = map[string]string{"http": "80", "https": "443"}[u.Scheme]
	}
	if port == "" {
		return ""
	}
	hp := net.JoinHostPort(u.Hostname(), port)
	if u.Scheme == "https" {
		return "https://" + hp
	}
	return hp
}

// State is a unit's LoadState, ActiveState and SubState, e.g.
// loaded active/running.
type State struct{ Load, Active, Sub string }

// States asks systemd for the state of units (one systemctl per scope).
// Units it can't ask about are left out.
func States(units []Unit) map[string]State {
	out := map[string]State{}
	byScope, seen := map[string][]string{}, map[string]bool{}
	for _, u := range units {
		if !seen[u.Key()] {
			seen[u.Key()] = true
			byScope[u.Scope] = append(byScope[u.Scope], u.Name)
		}
	}
	for scope, names := range byScope {
		args := []string{"show", "--property=LoadState,ActiveState,SubState", "--"}
		if scope == "user" {
			args = append([]string{"--user"}, args...)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		b, err := exec.CommandContext(ctx, "systemctl", append(args, names...)...).Output()
		cancel()
		if err != nil {
			continue
		}
		for i, st := range parseShow(string(b)) {
			if i < len(names) {
				out[scope+"/"+names[i]] = st
			}
		}
	}
	return out
}

// parseShow splits `systemctl show` output into one State per unit, in
// the order asked (blocks are separated by blank lines).
func parseShow(out string) []State {
	var res []State
	for _, block := range strings.Split(strings.TrimSpace(out), "\n\n") {
		var st State
		for _, line := range strings.Split(block, "\n") {
			k, v, _ := strings.Cut(line, "=")
			switch k {
			case "LoadState":
				st.Load = v
			case "ActiveState":
				st.Active = v
			case "SubState":
				st.Sub = v
			}
		}
		res = append(res, st)
	}
	return res
}

// Binary is the program a unit runs (ExecStart='s path; "" when systemd
// doesn't know the unit or it has no ExecStart=).
func Binary(u Unit) string {
	args := []string{"show", "--property=ExecStart", "--value", "--", u.Name}
	if u.Scope == "user" {
		args = append([]string{"--user"}, args...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "systemctl", args...).Output()
	if err != nil {
		return ""
	}
	return execPath(string(b))
}

// execPath reads the path out of `systemctl show -p ExecStart --value`:
// "{ path=/usr/bin/app ; argv[]=/usr/bin/app serve ; … }".
func execPath(show string) string {
	_, rest, ok := strings.Cut(show, "path=")
	if !ok {
		return ""
	}
	if i := strings.IndexAny(rest, " ;\n"); i >= 0 {
		rest = rest[:i]
	}
	if !strings.HasPrefix(rest, "/") {
		return ""
	}
	return rest
}
