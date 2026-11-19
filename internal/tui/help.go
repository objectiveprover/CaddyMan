package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"caddyman/internal/config"
)

// helpSection is a heading and its rows on the Keybindings page; the filter
// keeps a heading while any of its rows match.
type helpSection struct {
	title string // "" for the paths at the bottom
	rows  []helpRow
}

// helpRow is one line of the Keybindings page. hang is the column its
// description starts at, so a wrapped description continues under it
// instead of under the keys (0: wrap to the left edge).
type helpRow struct {
	text string
	hang int
}

// helpKeyW is the width of the key column on the Keybindings page.
const helpKeyW = 20

func (m *Model) showHelp() tea.Cmd {
	var secs []helpSection
	section := func(t string) { secs = append(secs, helpSection{title: t}) }
	rowHang := func(r string, hang int) {
		secs[len(secs)-1].rows = append(secs[len(secs)-1].rows, helpRow{r, hang})
	}
	row := func(r string) { rowHang(r, 0) }
	descCol := len(gutter) + helpKeyW + len(colGap)
	add := func(k, d string) { rowHang(gutter+sKey.Render(pad(k, helpKeyW))+colGap+d, descCol) }

	section("Navigation")
	add("C-n / n / ↓", "Next site")
	add("C-p / p / ↑", "Previous site")
	add("C-v / M-v", "Page down / up")
	add("M-↑ / M-↓", "First / last")
	add("C-g / ESC ESC", "Cancel, or clear the filter")
	add("ESC key", "Works like Meta (ESC x is the same as M-x)")
	add("C-x C-s", "Apply changes: copy the saved changes into Caddy's directory and reload Caddy (asks for your password)")
	add("C-x C-c", "Quit (asks first when changes aren't applied)")
	section("Commands")
	nameW := 0
	for _, c := range commands {
		if c.keys != "" {
			nameW = max(nameW, ansi.StringWidth(c.name))
		}
	}
	for _, c := range commands {
		if c.keys == "" {
			continue
		}
		var ks []string
		for _, k := range strings.Fields(c.keys) {
			ks = append(ks, emacsKey(k))
		}
		// The command's description wraps under itself, not under its name.
		rowHang(gutter+sKey.Render(pad(strings.Join(ks, " / "), helpKeyW))+colGap+
			sDim.Render(pad(c.name, nameW)+colGap)+c.desc, descCol+nameW+len(colGap))
	}
	row("")
	row(gutter + sDim.Render("Press M-x to see all available commands (Check Caddy setup, Show hosts lines, …)"))
	section("Viewer (site file, status, setup check)")
	add("C-n C-p C-v M-v", "Scroll (also SPC / DEL)")
	add("M-↑ / M-↓", "Top / bottom")
	add("C-s / C-r", "Search forward / backward")
	add("w", "Turn line wrapping on or off")
	add("RET / q / C-g", "Back to the list")
	section("Keybindings (?)")
	add("C-s", "Filter (separate words with spaces)")
	add("C-g", "Clear the filter")
	add("RET / q", "Back to the list")
	section("Add Site (c) / Edit Site (e) form")
	add("TAB C-n / S-TAB C-p", "Next / previous field")
	add("SPC / C-f / C-b", "Go through a choice field's options")
	add("C-c C-c", "Save: write the site's file in CaddyMan's directory and check it with Caddy (put back if Caddy refuses); C-x C-s then applies it")
	add("C-c C-p", "Show or hide the preview of the site's file")
	add("SPC on Service", "Go through ServiceMan's tracked services; picking one fills an empty Upstream from its health URL")
	add("C-g", "Cancel")
	section("")
	row(sDim.Render("Config: " + config.Tilde(config.Path())))
	row(sDim.Render("Site files: " + config.Tilde(config.SitesDir()) + " (applied to " + m.cfg.CaddySitesDirPath() + ")"))
	row(sDim.Render("Caddy's Caddyfile: " + m.cfg.CaddyfilePath()))
	row(sDim.Render("ServiceMan's config: " + config.Tilde(m.cfg.ServicemanConfigPath())))

	p := newPager("Keybindings", "")
	p.follow = false
	p.filterFn = func(q string) ([]string, []int) { return helpLines(secs, q) }
	p.applyFilter("")
	m.pager, m.mode = p, modePager
	return nil
}

// helpLines lays out the sections, keeping only the rows that match q (all
// its words, like the other filters) under their headings. hangs holds each
// line's description column (see helpRow).
func helpLines(secs []helpSection, q string) (lines []string, hangs []int) {
	for _, s := range secs {
		var rows []helpRow
		for _, r := range s.rows {
			if matchAll(q, ansi.Strip(r.text)) {
				rows = append(rows, r)
			}
		}
		if len(rows) == 0 {
			continue
		}
		if len(lines) > 0 {
			lines, hangs = append(lines, ""), append(hangs, 0)
		}
		if s.title != "" {
			lines, hangs = append(lines, sBold.Render(s.title)), append(hangs, 0)
		}
		for _, r := range rows {
			lines, hangs = append(lines, r.text), append(hangs, r.hang)
		}
	}
	return lines, hangs
}
