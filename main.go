// CaddyMan — manage the sites (reverse proxies) your Caddy serves for your
// self-hosted services, from a TUI.
package main

import (
	_ "embed"
	"fmt"
	"log"
	"os"
	"strings"

	"caddyman/internal/caddy"
	"caddyman/internal/config"
	"caddyman/internal/hosts"
	"caddyman/internal/tui"
)

// configTemplate is the default config (also installed by `make install`);
// `caddyman config update` adds its new fields to an existing config.
//
//go:embed config.template.json
var configTemplate []byte

const usage = `CaddyMan — manage the sites Caddy serves for your self-hosted services

Usage:
  caddyman                       open the TUI
  caddyman list                  list the sites
  caddyman apply                 copy the sites into Caddy's directory and reload Caddy
                                 (with sudo; also after editing the config by hand)
  caddyman check                 check that Caddy runs and imports CaddyMan's sites
  caddyman hosts                 print /etc/hosts lines for the sites
  caddyman paths                 show where the files are
  caddyman config version        show the config's version and the current one
  caddyman config update         add new settings to the config and raise its version
                                 (never changes or removes existing settings)
  caddyman config path           print where the config file is
  caddyman config template       print the default config

Config: ` + "%s" + `
`

func main() {
	log.SetFlags(0)
	args := os.Args[1:]
	if len(args) == 0 {
		if err := tui.Run(); err != nil {
			log.Fatal(err)
		}
		return
	}
	var err error
	switch args[0] {
	case "list", "ls":
		err = list()
	case "apply":
		err = apply()
	case "check":
		err = check()
	case "hosts":
		err = hostsCmd()
	case "paths":
		err = paths()
	case "config":
		err = configCmd(args[1:])
	case "help", "-h", "--help":
		fmt.Printf(usage, config.Path())
	default:
		fmt.Fprintf(os.Stderr, usage, config.Path())
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "caddyman:", err)
		os.Exit(1)
	}
}

// warnOld warns when the config is older than this CaddyMan.
func warnOld() {
	if v, err := config.FileVersion(config.Path()); err == nil && v < config.CurrentVersion {
		fmt.Fprintf(os.Stderr, "caddyman: your config is version %d, current is %d — run `caddyman config update` to add the new settings\n", v, config.CurrentVersion)
	}
}

func list() error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	warnOld()
	if len(c.Sites) == 0 {
		fmt.Println("no sites yet (add them in the TUI: caddyman)")
		return nil
	}
	flag := func(on bool, s string) string {
		if on {
			return s
		}
		return "-"
	}
	dash := func(s string) string {
		if s == "" {
			return "-"
		}
		return s
	}
	p, _ := caddy.Pending(c)
	fmt.Printf("%-32s %-5s %-7s %-24s %-28s %-5s %-7s %s\n", "SITE", "STATE", "APPLIED", "TARGET", "SERVICE", "HTTPS", "TRUSTED", "REDIRECT")
	for _, s := range c.Sorted() {
		svc := ""
		if s.Service != "" {
			svc = s.Service + " (" + s.ServiceScope + ")"
		}
		applied := "yes"
		if p.Has(c.Address(s)) {
			applied = "no"
		}
		fmt.Printf("%-32s %-5s %-7s %-24s %-28s %-5s %-7s %s\n", c.Address(s), map[bool]string{true: "on", false: "off"}[s.Enabled], applied,
			config.Tilde(s.Target()), dash(svc), flag(s.HTTPS, "yes"), flag(s.TrustedOnly, "yes"), dash(s.RedirectRoot))
	}
	if len(p.Removed) > 0 {
		fmt.Printf("\nnot applied yet, still in Caddy: %s (deleted or disabled)\n", strings.Join(p.Removed, ", "))
	}
	return nil
}

func apply() error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	warnOld()
	seen := map[string]bool{}
	for _, s := range c.Sites {
		addr := c.Address(s)
		if seen[addr] {
			return fmt.Errorf("%s is in the config twice", addr)
		}
		seen[addr] = true
		if err := c.Validate(addr, s); err != nil {
			return fmt.Errorf("%s: %w", addr, err)
		}
	}
	saved, err := caddy.SaveLocal(c, nil)
	if err != nil {
		return err
	}
	if !saved.Checked {
		fmt.Printf("note: the sites weren't checked before applying (%s)\n", saved.Unchecked)
	}
	p, err := caddy.Pending(c)
	if err != nil {
		return err
	}
	if p.Count() == 0 {
		fmt.Println("no changes to apply; reloading Caddy anyway")
	} else {
		for _, l := range []struct {
			what  string
			addrs []string
		}{{"add", p.Added}, {"change", p.Changed}, {"remove", p.Removed}} {
			if len(l.addrs) > 0 {
				fmt.Printf("%s: %s\n", l.what, strings.Join(l.addrs, ", "))
			}
		}
	}
	cmd, err := caddy.DeployCmd(c, false)
	if err != nil {
		return err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Apply changes failed: %w", err)
	}
	if m := caddy.Missing(c); len(m) > 0 {
		return fmt.Errorf("Caddy's config doesn't have %s: your Caddyfile doesn't import them; run caddyman check", strings.Join(m, ", "))
	}
	fmt.Println("applied")
	return nil
}

func check() error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	warnOld()
	failed := 0
	for _, ch := range caddy.CheckSetup(c) {
		if ch.OK {
			fmt.Println("✓ " + ch.Text)
			continue
		}
		failed++
		fmt.Println("✗ " + ch.Text)
		if ch.Fix != "" {
			fmt.Println("  → " + ch.Fix)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d problem(s)", failed)
	}
	return nil
}

func hostsCmd() error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	ip, err := hosts.LanIP(c.LanIP)
	if err != nil {
		return err
	}
	for _, l := range hosts.Lines(c, ip) {
		fmt.Println(l)
	}
	return nil
}

func paths() error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	fmt.Printf("config:       %s\nsite files:   %s\nCaddy's copy: %s\ncaddyfile:    %s\nroot cert:    %s\nserviceman:   %s\n",
		config.Path(), config.SitesDir(), c.CaddySitesDirPath(), c.CaddyfilePath(), c.RootCertPath(), c.ServicemanConfigPath())
	return nil
}

func configCmd(args []string) error {
	const use = "usage: caddyman config version|update|path|template [CONFIG]"
	if len(args) == 0 || len(args) > 2 {
		return fmt.Errorf(use)
	}
	path := config.Path()
	if len(args) == 2 {
		path = config.ExpandTilde(args[1])
	}
	switch args[0] {
	case "path":
		fmt.Println(path)
	case "template":
		os.Stdout.Write(configTemplate)
	case "version":
		v, err := config.FileVersion(path)
		if err != nil {
			return err
		}
		switch {
		case v < config.CurrentVersion:
			fmt.Printf("%s: version %d, current is %d (caddyman config update adds the new settings)\n", config.Tilde(path), v, config.CurrentVersion)
		case v > config.CurrentVersion:
			fmt.Printf("%s: version %d, newer than this CaddyMan's %d\n", config.Tilde(path), v, config.CurrentVersion)
		default:
			fmt.Printf("%s: version %d (current)\n", config.Tilde(path), v)
		}
	case "update":
		res, err := config.UpdateFromTemplate(path, configTemplate)
		if err != nil {
			return err
		}
		if len(res.Added) == 0 && res.OldVersion == res.NewVersion {
			fmt.Printf("%s is up to date (version %d)\n", config.Tilde(path), res.NewVersion)
			return nil
		}
		msg := fmt.Sprintf("updated %s: version %d → %d", config.Tilde(path), res.OldVersion, res.NewVersion)
		if len(res.Added) > 0 {
			msg += ", added " + strings.Join(res.Added, ", ")
		}
		fmt.Println(msg)
	default:
		return fmt.Errorf(use)
	}
	return nil
}
