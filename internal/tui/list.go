package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"caddyman/internal/caddyfile"
	"caddyman/internal/config"
)

func (m *Model) listKey(msg tea.KeyMsg) tea.Cmd {
	k := msg.String()
	n := len(m.rows())
	page := max(1, m.h-8)
	switch k {
	case "ctrl+n", "n", "down":
		m.cursor = min(m.cursor+1, n-1)
	case "ctrl+p", "p", "up":
		m.cursor = max(m.cursor-1, 0)
	case "ctrl+v", "pgdown":
		m.cursor = min(m.cursor+page, n-1)
	case "alt+v", "pgup":
		m.cursor = max(m.cursor-page, 0)
	case "alt+up", "alt+<", "home":
		m.cursor = 0
	case "alt+down", "alt+>", "end":
		m.cursor = n - 1
	case "ctrl+g", "esc":
		if m.filter != "" {
			m.filter = ""
			m.setMsg("Filter cleared")
		} else {
			m.setMsg("Quit")
		}
	default:
		for _, c := range commands {
			for _, ck := range strings.Fields(c.keys) {
				if ck == k {
					return runCommand(m, c)
				}
			}
		}
		m.setErr("%s is undefined (? for help, M-x for commands)", emacsKey(k))
	}
	m.clampCursor()
	return nil
}

// caddyCell is Caddy's state for the right of the top bar.
func (m *Model) caddyCell() string {
	label := sDim.Render("Caddy: ")
	switch {
	case !m.statusKnown:
		return label + sDim.Render("…")
	case !m.caddy.Found:
		return label + sRed.Render("not installed") + sDim.Render(" (M-x Check Caddy setup)")
	case m.caddy.Running:
		v := ""
		if m.caddyVersion != "" {
			v = sDim.Render(" v" + m.caddyVersion)
		}
		return label + sGreen.Render("● running") + v
	case m.caddy.Unit == "failed":
		return label + sRed.Render("✖ failed") + sDim.Render(" (i for status)")
	case m.caddy.Unit == "activating" || m.caddy.Unit == "deactivating":
		return label + sYellow.Render("◐ "+m.caddy.Unit)
	default:
		return label + sYellow.Render("○ stopped")
	}
}

// ------------------------------------------------------------------ view

type column struct {
	title string
	width int
	prio  int // lower = dropped first when narrow
}

func yesNo(on bool) string {
	if on {
		return "yes"
	}
	return sDim.Render("-")
}

func (m *Model) listView(w, h int) string {
	rows := m.rows()
	var b strings.Builder

	keyed := func(name, key string) string { return sDim.Render(name+" (") + sKey.Render(key) + sDim.Render(")") }
	left := []string{keyed("Add site", "c")}
	switch n := m.pending.Count(); {
	case n == 1:
		left = append(left, sYellow.Render("1 change to apply (")+sKey.Render("C-x C-s")+sYellow.Render(")"))
	case n > 1:
		left = append(left, sYellow.Render(fmt.Sprintf("%d changes to apply (", n))+sKey.Render("C-x C-s")+sYellow.Render(")"))
	}
	if m.filter != "" {
		left = append(left, sDim.Render("Filter: ")+sKey.Render(m.filter))
	}
	if m.busy != "" {
		left = append(left, sYellow.Render(m.busy+"…"))
	}
	// Caddy's state stays on the right; the left side gives way.
	b.WriteString(topBar(w, "Sites", left, m.caddyCell()) + "\n")

	cols := []column{
		{"STATE", 5, 7}, {"TARGET", 22, 6}, {"SERVICE", 26, 5}, {"HTTPS", 5, 3},
		{"TRUSTED", 7, 2}, {"REDIRECT", 10, 1},
	}
	siteW := func() int {
		t := 3 // marker (2) + spare (1)
		for _, c := range cols {
			t += len(colGap) + c.width
		}
		return w - t
	}
	count := fmt.Sprintf("SITE [%d]", len(m.cfg.Sites))
	if m.filter != "" {
		count = fmt.Sprintf("SITE [%d/%d]", len(rows), len(m.cfg.Sites))
	}
	// The SITE column is as wide as the longest address (all sites, so it
	// doesn't jump while filtering), capped at maxSiteW.
	const maxSiteW = 36
	need := ansi.StringWidth(count)
	for _, s := range m.cfg.Sites {
		need = max(need, ansi.StringWidth(m.cfg.Address(s))+2) // " ✎"
	}
	need = min(need, maxSiteW)
	for siteW() < min(need, 18) && len(cols) > 1 {
		lo := 0
		for i, c := range cols {
			if c.prio < cols[lo].prio {
				lo = i
			}
		}
		cols = append(cols[:lo], cols[lo+1:]...)
	}
	sw := max(1, min(need, siteW()))
	hdr := sHeader.Render(gutter + pad(count, sw))
	for _, c := range cols {
		hdr += sHeader.Render(colGap + pad(c.title, c.width))
	}
	b.WriteString(hdr + "\n")

	detailH := 10
	listH := h - 3 - detailH // title, separator, column header
	if listH < 3 {
		listH, detailH = h-3, 0
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+listH {
		m.offset = m.cursor - listH + 1
	}
	if len(rows) == 0 {
		msg := "  No sites yet. Press c to add one."
		if m.filter != "" {
			msg = "  No sites match the filter (C-g clears it)."
		}
		b.WriteString(sDim.Render(msg) + "\n")
	}
	for i := m.offset; i < len(rows) && i < m.offset+listH; i++ {
		s := rows[i]
		cells := map[string]string{
			"STATE":    sGreen.Render("● on"),
			"TARGET":   config.Tilde(s.Target()),
			"HTTPS":    yesNo(s.HTTPS),
			"TRUSTED":  yesNo(s.TrustedOnly),
			"REDIRECT": s.RedirectRoot,
			"SERVICE":  m.serviceCell(s),
		}
		if s.RedirectRoot == "" {
			cells["REDIRECT"] = sDim.Render("-")
		}
		if s.Kind == config.Static {
			cells["TARGET"] = sDim.Render("files ") + cells["TARGET"]
		}
		addr := m.cfg.Address(s)
		name := addr
		if !s.Enabled {
			cells["STATE"] = sDim.Render("○ off")
			name = sDim.Render(name)
		}
		if i == m.cursor {
			name = sHighlight.Render(addr)
		}
		mark := ""
		if m.pending.Has(addr) {
			mark = sYellow.Render(" ✎")
		}
		line := markedName(name, mark, sw, false)
		for _, c := range cols {
			line += colGap + pad(cells[c.title], c.width)
		}
		b.WriteString(cursorMark(i == m.cursor) + line + "\n")
	}
	used := min(len(rows)-m.offset, listH)
	if len(rows) == 0 {
		used = 1
	}
	for i := used; i < listH; i++ {
		b.WriteString("\n")
	}
	if detailH > 0 {
		b.WriteString(m.detailView(w, detailH))
	}
	return strings.TrimRight(b.String(), "\n")
}

// unitState is the state of the site's service, colored, e.g. "● active".
func (m *Model) unitState(s config.Site) string {
	st, ok := m.units[s.ServiceKey()]
	switch {
	case !ok:
		return sDim.Render("…")
	case st.Load == "not-found":
		return sRed.Render("✖ not found")
	case st.Active == "active":
		return sGreen.Render("● active")
	case st.Active == "failed":
		return sRed.Render("✖ failed")
	case st.Active == "activating" || st.Active == "deactivating" || st.Active == "reloading":
		return sYellow.Render("◐ " + st.Active)
	default:
		return sDim.Render("○ " + st.Active)
	}
}

// serviceCell is the SERVICE column: the state, then the unit's name.
func (m *Model) serviceCell(s config.Site) string {
	if s.Service == "" {
		return sDim.Render("-")
	}
	return m.unitState(s) + " " + strings.TrimSuffix(s.Service, ".service")
}

// detailView is the box under the list: the selected site's address and
// URL, its service, then its Caddyfile block.
func (m *Model) detailView(w, h int) string {
	out := []string{rule(w)}
	s := m.selected()
	if s == nil {
		return strings.Join(out, "\n")
	}
	title := gutter + sBold.Render(m.cfg.Address(*s))
	if s.Enabled {
		title += sDim.Render("  ") + s.URL(m.cfg.DomainSuffix)
	} else {
		title += sYellow.Render("  disabled: not in Caddy's config (x enables it)")
	}
	out = append(out, trunc(title, w))
	if s.Service != "" {
		out = append(out, trunc(gutter+sDim.Render("Service: ")+s.Service+sDim.Render(" ("+s.ServiceScope+")  ")+m.unitState(*s)+
			sDim.Render("  (s for its status)"), w))
	}
	block := strings.Split(strings.TrimRight(caddyfile.Site(m.cfg, m.previewSite(*s)), "\n"), "\n")
	room := h - len(out)
	if len(block) > room {
		block = append(block[:room-1], sDim.Render("…  (v shows the whole file)"))
	}
	for _, l := range block {
		out = append(out, trunc(gutter+sDim.Render(strings.ReplaceAll(l, "\t", "    ")), w))
	}
	return strings.Join(out, "\n")
}
