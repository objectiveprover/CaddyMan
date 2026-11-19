package caddy

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"caddyman/internal/caddyfile"
	"caddyman/internal/config"
)

// deployScript is Apply changes, run as root. It gets everything as
// arguments ($1 CaddyMan's sites, $2 caddy_sites_dir, $3 the main
// Caddyfile, $4 the caddy command), never spliced into its text. It only
// touches files that start with CaddyMan's marker, and puts the old ones
// back (and reloads again) if Caddy rejects the result.
const deployScript = `set -u
src=$1 dst=$2 caddyfile=$3 bin=$4
marker='` + caddyfile.Marker + `'
owned() { [ -f "$1" ] && [ "$(head -c ${#marker} "$1")" = "$marker" ]; }
active() { systemctl is-active --quiet caddy; }

install -d -m 755 "$dst" || exit 1
backup=$(mktemp -d) || exit 1
trap 'rm -rf "$backup"' EXIT
for f in "$dst"/*.caddy; do if owned "$f"; then cp -p "$f" "$backup/" || exit 1; fi; done

restore() {
	echo "caddyman: putting the previous site files back" >&2
	for f in "$dst"/*.caddy; do if owned "$f"; then rm -f "$f"; fi; done
	for f in "$backup"/*.caddy; do if [ -e "$f" ]; then cp -p "$f" "$dst/"; fi; done
	if active; then systemctl reload caddy; fi
	exit 1
}

for f in "$src"/*.caddy; do
	[ -e "$f" ] || continue
	t=$dst/$(basename "$f")
	if [ -e "$t" ] && ! owned "$t"; then
		echo "caddyman: $t wasn't written by CaddyMan; nothing was changed" >&2
		exit 1
	fi
done
for f in "$dst"/*.caddy; do
	if owned "$f" && [ ! -e "$src/$(basename "$f")" ]; then
		echo "removing $f"
		rm -f "$f" || restore
	fi
done
for f in "$src"/*.caddy; do
	[ -e "$f" ] || continue
	if ! cmp -s "$f" "$dst/$(basename "$f")"; then
		echo "copying $(basename "$f") to $dst"
		install -m 644 "$f" "$dst/" || restore
	fi
done

echo "checking the config"
"$bin" adapt --config "$caddyfile" --adapter caddyfile >/dev/null || restore
if active; then
	echo "reloading Caddy"
	systemctl reload caddy || restore
else
	echo "Caddy isn't running; it will serve the sites when it starts"
fi
`

// sudoArgs is the command that runs the script as root: sudo, or
// $CADDYMAN_SUDO when set (tests run the script without root with it set
// to "env").
func sudoArgs(nonInteractive bool) []string {
	if s := os.Getenv("CADDYMAN_SUDO"); s != "" {
		return strings.Fields(s)
	}
	if nonInteractive {
		return []string{"sudo", "-n"}
	}
	return []string{"sudo"}
}

func deployArgs(c *config.Config) ([]string, error) {
	bin, err := Find(c.CaddyBinPath())
	if err != nil {
		return nil, err
	}
	return []string{"sh", "-c", deployScript, "caddyman-apply",
		config.SitesDir(), c.CaddySitesDirPath(), c.CaddyfilePath(), bin}, nil
}

// DeployCmd is Apply changes for a terminal: sudo can ask for your
// password. With pause, a failure waits for Enter so its output can be
// read before the TUI comes back.
func DeployCmd(c *config.Config, pause bool) (*exec.Cmd, error) {
	args, err := deployArgs(c)
	if err != nil {
		return nil, err
	}
	args = append(sudoArgs(false), args...)
	if !pause {
		return exec.Command(args[0], args[1:]...), nil
	}
	wrapper := `"$@"; rc=$?
if [ $rc -ne 0 ]; then echo; printf 'caddyman: Apply changes failed (exit %s); press Enter to return ' $rc; read _; fi
exit $rc`
	return exec.Command("sh", append([]string{"-c", wrapper, "caddyman"}, args...)...), nil
}

// DeployQuick runs Apply changes without a terminal when sudo doesn't need
// a password (it's cached, or NOPASSWD). needAuth means it does: use
// DeployCmd instead. out is the script's output.
func DeployQuick(c *config.Config) (out string, needAuth bool, err error) {
	args, err := deployArgs(c)
	if err != nil {
		return "", false, err
	}
	sudo := sudoArgs(true)
	if sudo[0] == "sudo" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		ok := exec.CommandContext(ctx, "sudo", "-n", "true").Run() == nil
		cancel()
		if !ok {
			return "", true, nil
		}
	}
	args = append(sudo, args...)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var b bytes.Buffer
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdout, cmd.Stderr = &b, &b
	err = cmd.Run()
	return b.String(), false, err
}

// Missing are the enabled sites the running config doesn't have after an
// apply: Caddy's main Caddyfile doesn't import caddy_sites_dir. nil when
// it can't tell.
func Missing(c *config.Config) []string {
	bin, err := Find(c.CaddyBinPath())
	if err != nil {
		return nil
	}
	js, err := Adapt(bin, c.CaddyfilePath())
	if err != nil {
		return nil
	}
	var out []string
	for _, s := range c.Sorted() {
		if s.Enabled && !strings.Contains(js, strconv.Quote(c.Address(s))) {
			out = append(out, c.Address(s))
		}
	}
	return out
}
