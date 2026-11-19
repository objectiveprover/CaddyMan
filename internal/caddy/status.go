package caddy

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// UnitName is the systemd unit Caddy runs as (system scope).
const UnitName = "caddy.service"

// Status is what the TUI shows about Caddy.
type Status struct {
	Found   bool   // the caddy command exists
	Running bool   // caddy.service is active
	Unit    string // caddy.service's ActiveState ("" if unknown)
}

// unitState is `systemctl is-active caddy.service`.
func unitState() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, "systemctl", "is-active", UnitName).Output()
	return strings.TrimSpace(string(out))
}

// GetStatus checks the caddy command and caddy.service.
func GetStatus(bin string) Status {
	_, err := Find(bin)
	st := Status{Found: err == nil, Unit: unitState()}
	st.Running = st.Unit == "active" || st.Unit == "reloading"
	return st
}
