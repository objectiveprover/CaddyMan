package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func (m *Model) footer() string {
	var b strings.Builder
	b.WriteString(rule(m.w) + "\n")
	switch {
	case m.prompt != nil:
		b.WriteString(m.prompt.view(m.w))
	case m.msg != "":
		st := sDim
		if m.msgErr {
			st = sRed
		}
		b.WriteString(trunc(gutter+st.Render(m.msg), m.w))
	default:
		b.WriteString(m.keyHints())
	}
	return b.String()
}

// keyHints is the bottom line when the echo area is empty: the main keys of
// the current mode.
func (m *Model) keyHints() string {
	var keys []string
	switch m.mode {
	case modePager:
		// Wrap shows its key in the top bar.
		keys = []string{"Back", "RET/q", "Search", "C-s", "Top/Bottom", "M-↑/M-↓"}
		if m.pager.filterFn != nil {
			keys[2] = "Filter"
		}
	case modeForm:
		keys = []string{"Cancel", "C-g", "Save", "C-c C-c", "Field", "TAB/S-TAB", "Cycle choice", "SPC", "Preview", "C-c C-p"}
	default:
		keys = []string{"Help", "?", "Quit", "q", "Commands", "M-x", "Add", "c", "Edit", "e", "Apply", "C-x C-s", "Disable/Enable", "x", "Filter", "C-s"}
	}
	return hintLine(m.w, "", keys)
}

// hintLine renders "Name (key)   Name (key) …" (name/key pairs, like the
// details box labels) after an optional lead, dropping pairs from the end
// when they don't fit.
func hintLine(w int, lead string, kv []string) string {
	render := func(d, k string) string {
		if k == "" {
			return sDim.Render(d)
		}
		return sDim.Render(d+" (") + sKey.Render(k) + sDim.Render(")")
	}
	sep := sDim.Render("   ")
	out := gutter
	if lead != "" {
		out += sBold.Render(lead) + "   "
	}
	for i := 0; i+1 < len(kv); i += 2 {
		next := render(kv[i], kv[i+1])
		if i > 0 {
			next = sep + next
		}
		if i > 0 && ansi.StringWidth(out+next) > w {
			break
		}
		out += next
	}
	return trunc(out, w)
}

// topBar is the first line of every view plus the separator under it:
// CADDYMAN, the view's name and its context on the left, the view's main
// indicators on the right. The left side gives way when space runs out.
func topBar(w int, view string, left []string, right string) string {
	items := []string{gutter + sTitle.Render("CADDYMAN")}
	if view != "" {
		items = append(items, sBold.Render(view))
	}
	items = append(items, left...)
	if right != "" {
		right += gutter
	}
	// Four spaces between items; two when narrow.
	var l string
	for _, gap := range []string{"    ", "  "} {
		l = strings.Join(items, gap)
		if ansi.StringWidth(l)+ansi.StringWidth(right)+2 <= w {
			break
		}
	}
	l = trunc(l, max(0, w-ansi.StringWidth(right)-2))
	bar := l + strings.Repeat(" ", max(0, w-ansi.StringWidth(l)-ansi.StringWidth(right))) + right
	return bar + "\n" + heavyRule(w)
}
