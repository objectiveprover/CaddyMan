// Package caddy talks to the Caddy you run (as a normal system service):
// its command (cli.go) and service (status.go), saving the site files in
// CaddyMan's directory (local.go), what Apply changes would change
// (pending.go) and applying them as root (deploy.go), checking the setup
// (check.go) and its root certificate (rootcert.go). Installing and running
// Caddy is up to you.
package caddy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Find resolves the caddy command (a name in PATH or a path).
func Find(bin string) (string, error) {
	p, err := exec.LookPath(bin)
	if err != nil {
		return "", fmt.Errorf("caddy isn't installed (no %q in PATH)", bin)
	}
	return p, nil
}

// Version is Caddy's version without the "v", e.g. 2.10.2.
func Version(bin string) (string, error) {
	out, err := run(bin, 10*time.Second, "version")
	if err != nil {
		return "", err
	}
	f := strings.Fields(out)
	if len(f) == 0 {
		return "", fmt.Errorf("%s version printed nothing", bin)
	}
	return strings.TrimPrefix(f[0], "v"), nil
}

// run runs caddy and returns its standard output, or its error message.
func run(bin string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var out, errOut bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	if err == nil {
		return out.String(), nil
	}
	if ctx.Err() != nil {
		return "", fmt.Errorf("caddy %s took longer than %s", args[0], timeout)
	}
	if msg := caddyError(errOut.String() + out.String()); msg != "" {
		return "", errors.New(msg)
	}
	return "", fmt.Errorf("caddy %s: %w", args[0], err)
}

// Adapt turns the main Caddyfile (with everything it imports) into Caddy's
// JSON config. It only parses: nothing is provisioned, so running it as
// you works even though Caddy runs as another user.
func Adapt(bin, caddyfile string) (string, error) {
	return run(bin, 30*time.Second, "adapt", "--config", caddyfile, "--adapter", "caddyfile")
}

var (
	reErrLine = regexp.MustCompile(`(?m)^Error: (.+)$`)
	reJSONMsg = regexp.MustCompile(`"level":"error"[^\n]*?"(?:error|msg)":"((?:[^"\\]|\\.)*)"`)
)

// caddyError picks the message out of Caddy's output: the "Error: …" line
// it ends with, else the last logged error.
func caddyError(out string) string {
	if m := reErrLine.FindAllStringSubmatch(out, -1); len(m) > 0 {
		return shortenChain(strings.TrimSpace(m[len(m)-1][1]))
	}
	if m := reJSONMsg.FindAllStringSubmatch(out, -1); len(m) > 0 {
		msg := m[len(m)-1][1]
		if err := json.Unmarshal([]byte(`"`+msg+`"`), &msg); err != nil {
			msg = strings.ReplaceAll(msg, `\"`, `"`)
		}
		msg = strings.TrimSpace(msg)
		if strings.Contains(msg, `{"`) {
			// A reload error quotes the instance's JSON reply: drop its closing "}.
			msg = strings.TrimRight(msg, `"}`)
		}
		return shortenChain(msg)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// shortenChain keeps the end of a long chain of wrapped errors ("loading
// http app module: provision http: … : invalid IP address: 'x'"), where
// the cause is.
func shortenChain(msg string) string {
	const keep = 4
	parts := strings.Split(msg, ": ")
	if len(parts) <= keep+1 {
		return msg
	}
	return "…: " + strings.Join(parts[len(parts)-keep:], ": ")
}
