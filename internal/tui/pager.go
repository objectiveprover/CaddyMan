package tui

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

var (
	reErr  = regexp.MustCompile(`(?i)\b(error|err|fail(ed|ure)?|fatal|panic|crit(ical)?|emerg|alert|exception|traceback)\b`)
	reWarn = regexp.MustCompile(`(?i)\b(warn(ing)?|deprecated)\b`)
)

type pager struct {
	title     string // the view's name in the top bar, e.g. Caddy status
	subject   string // e.g. caddy.service
	lastShown int    // 1-based index of the last line on screen
	lines     []string
	hang      []int // per line: where wrapped text continues (nil: column 0)
	offset    int
	follow    bool
	wrap      bool
	search    string
	w, h      int

	// filterable text (the Keybindings page): C-s filters instead of searching.
	filterFn func(q string) ([]string, []int) // the lines that match q, and their hangs
	filter   string

	loading bool // static text whose command is still running
}

func newPager(title, subject string) *pager {
	return &pager{title: title, subject: subject, wrap: true}
}

// showText shows fixed lines in the pager.
func (m *Model) showText(title, subject string, lines []string) tea.Cmd {
	p := newPager(title, subject)
	p.setLines(lines)
	m.pager, m.mode = p, modePager
	return nil
}

// staticLoadedMsg is the output of a showStatic command.
type staticLoadedMsg struct {
	p     *pager
	lines []string
}

// showStatic shows the output of cmd (systemctl status) in the pager. The
// viewer opens right away; the command runs off the UI goroutine (reading
// the journal can take a moment).
func (m *Model) showStatic(title, subject string, cmd *exec.Cmd) tea.Cmd {
	p := newPager(title, subject)
	p.loading = true
	m.pager, m.mode = p, modePager
	return func() tea.Msg {
		out, _ := cmd.CombinedOutput()
		return staticLoadedMsg{p, strings.Split(strings.TrimRight(string(out), "\n"), "\n")}
	}
}

func (p *pager) setLines(l []string) {
	p.lines = l
	if p.follow {
		p.offset = p.bottomOffset()
	}
}

// ------------------------------------------------------------------ geometry

// segments is how line i is displayed: wrapped or truncated to the width.
// A line with a hang (a Keybindings row) wraps its description under the
// description's column rather than back at the left edge.
func (p *pager) segments(i int) []string {
	line := strings.ReplaceAll(p.lines[i], "\t", "    ")
	if p.w <= 0 {
		return []string{line}
	}
	if !p.wrap || ansi.StringWidth(line) <= p.w {
		return []string{trunc(line, p.w)}
	}
	if h := p.hangAt(i); h > 0 && p.w-h >= minHangWidth {
		head, tail := ansi.Truncate(line, h, ""), ansi.TruncateLeft(line, h, "")
		segs := strings.Split(ansi.Wrap(tail, p.w-h, ""), "\n")
		segs[0] = head + segs[0]
		for j := 1; j < len(segs); j++ {
			segs[j] = strings.Repeat(" ", h) + strings.TrimLeft(segs[j], " ")
		}
		return segs
	}
	return strings.Split(ansi.Hardwrap(line, p.w, true), "\n")
}

// minHangWidth is the narrowest a hanging description may get; below it the
// line wraps to the left edge like any other.
const minHangWidth = 10

func (p *pager) hangAt(i int) int {
	if i < len(p.hang) {
		return p.hang[i]
	}
	return 0
}

func (p *pager) lineHeight(i int) int { return len(p.segments(i)) }

func (p *pager) bottomOffset() int {
	if p.h <= 0 {
		return max(0, len(p.lines)-1)
	}
	used := 0
	for i := len(p.lines) - 1; i >= 0; i-- {
		used += p.lineHeight(i)
		if used > p.h {
			return i + 1
		}
	}
	return 0
}

func (p *pager) scroll(n int) {
	p.offset = max(0, min(p.offset+n, p.bottomOffset()))
	p.follow = p.offset >= p.bottomOffset()
}

// ------------------------------------------------------------------ keys

func (p *pager) key(m *Model, msg tea.KeyMsg) tea.Cmd {
	page := max(1, p.h-2)
	k := msg.String()
	if p.filterFn != nil {
		switch k {
		case "ctrl+s":
			p.startFilter(m)
			return nil
		case "ctrl+r": // no backward search here; C-s filters
			m.setErr("C-r is undefined here (C-s filters)")
			return nil
		case "ctrl+g", "esc":
			if p.filter != "" { // like the sites list: C-g clears the filter first
				p.applyFilter("")
				m.setMsg("Filter cleared")
				return nil
			}
		}
	}
	switch k {
	case "q", "ctrl+g", "esc", "enter":
		m.pager, m.mode = nil, modeList
		return m.refreshCmd()
	case "ctrl+n", "down", "n":
		p.scroll(1)
	case "ctrl+p", "up", "p":
		p.scroll(-1)
	case "ctrl+v", "pgdown", " ":
		p.scroll(page)
	case "alt+v", "pgup", "backspace":
		p.scroll(-page)
	case "alt+up", "alt+<", "<", "home":
		p.offset, p.follow = 0, false
	case "alt+down", "alt+>", ">", "end":
		p.offset, p.follow = p.bottomOffset(), true
	case "w":
		p.wrap = !p.wrap
		p.offset = min(p.offset, p.bottomOffset())
	case "ctrl+s":
		p.isearch(m, 1)
	case "ctrl+r":
		p.isearch(m, -1)
	case "ctrl+l": // Emacs's recenter: every key redraws the screen already
	default:
		m.setErr("%s is undefined in viewer (q to go back)", emacsKey(msg.String()))
	}
	return nil
}

// startFilter asks for a filter in the minibuffer, like the sites list's:
// the page shows only the matches as you type; C-g puts the old filter back.
func (p *pager) startFilter(m *Model) {
	old := p.filter
	pr := newPrompt("Filter: ", p.filter)
	pr.onChange = func(m *Model, v string) { p.applyFilter(v) }
	pr.onSubmit = func(m *Model, v string) tea.Cmd { p.applyFilter(strings.TrimSpace(v)); return nil }
	pr.onCancel = func(m *Model) { p.applyFilter(old) }
	m.prompt = pr
}

func (p *pager) applyFilter(q string) {
	p.filter = q
	lines, hang := p.filterFn(q)
	p.hang = hang
	p.setLines(lines)
	p.offset = 0
}

// isearch implements Emacs-style incremental search in the minibuffer.
func (p *pager) isearch(m *Model, dir int) {
	origin, origFollow := p.offset, p.follow
	pos := p.offset
	label := func(d int, failing bool) string {
		s := "I-search: "
		if d < 0 {
			s = "I-search backward: "
		}
		if failing {
			s = "Failing " + s
		}
		return s
	}
	pr := newPrompt(label(dir, false), "")
	find := func(q string, from, d int) {
		if q == "" {
			return
		}
		ql := strings.ToLower(q)
		for i := from; i >= 0 && i < len(p.lines); i += d {
			if strings.Contains(strings.ToLower(p.lines[i]), ql) {
				pos = i
				p.offset = max(0, min(i-p.h/3, p.bottomOffset()))
				p.follow = false
				pr.label = label(d, false)
				return
			}
		}
		pr.label = label(d, true)
	}
	pr.onChange = func(m *Model, v string) {
		p.search = v
		find(v, pos, dir)
	}
	pr.onKey = func(m *Model, k string) (bool, tea.Cmd) {
		switch k {
		case "ctrl+s":
			if pr.ti.Value() == "" && p.search != "" {
				pr.ti.SetValue(p.search)
				pr.ti.CursorEnd()
			}
			dir = 1
			find(pr.ti.Value(), pos+1, 1)
			return true, nil
		case "ctrl+r":
			if pr.ti.Value() == "" && p.search != "" {
				pr.ti.SetValue(p.search)
				pr.ti.CursorEnd()
			}
			dir = -1
			find(pr.ti.Value(), pos-1, -1)
			return true, nil
		}
		return false, nil
	}
	pr.onSubmit = func(m *Model, v string) tea.Cmd {
		p.search = v
		return nil
	}
	pr.onCancel = func(m *Model) {
		p.offset, p.follow = origin, origFollow
		p.search = ""
	}
	m.prompt = pr
}

// ------------------------------------------------------------------ view

// header is the viewer's top bar: what is shown and its state on the left,
// the position on the right. Each toggle shows its key; the other keys are
// in the bottom bar.
func (p *pager) header(w int) string {
	var left []string
	if p.subject != "" {
		left = append(left, sDim.Render(p.subject))
	}
	left = append(left, onOff("Wrap", "w", p.wrap))
	if p.filter != "" {
		left = append(left, sDim.Render("Filter: ")+sKey.Render(p.filter))
	}

	pos := "All"
	if len(p.lines) > 0 && p.bottomOffset() > 0 {
		switch {
		case p.offset == 0:
			pos = "Top"
		case p.offset >= p.bottomOffset():
			pos = "Bottom"
		default:
			pos = fmt.Sprintf("%d%%", p.offset*100/max(1, p.bottomOffset()))
		}
	}
	lines := "No lines"
	if len(p.lines) > 0 {
		lines = fmt.Sprintf("Lines %d–%d of %d", min(p.offset+1, len(p.lines)), max(p.offset+1, p.lastShown), len(p.lines))
	}
	return topBar(w, p.title, left, sDim.Render(lines)+"  "+sDim.Render(pos))
}

func (p *pager) view(w, h int) string {
	h = max(1, h-2)             // header + separator
	cw := max(1, w-len(gutter)) // text starts after the gutter, like the rest of the UI
	resize := p.w != cw || p.h != h
	p.w, p.h = cw, h
	if resize && p.follow {
		p.offset = p.bottomOffset()
	}
	var out []string
	p.lastShown = 0
	for i := p.offset; i < len(p.lines) && len(out) < h; i++ {
		for _, s := range p.segments(i) {
			if len(out) >= h {
				break
			}
			out = append(out, gutter+p.style(s))
		}
		p.lastShown = i + 1
	}
	if p.loading {
		out = append(out, gutter+sDim.Render("Loading…"))
	}
	if len(p.lines) == 0 && p.filter != "" {
		out = append(out, gutter+sDim.Render("No "+strings.ToLower(p.title)+" match the filter (C-g clears it)."))
	}
	// The header comes last so it reports what was actually laid out.
	return p.header(w) + "\n" + strings.Join(out, "\n")
}

func (p *pager) style(s string) string {
	if strings.Contains(s, "\x1b[") { // already styled (help text)
		return s
	}
	base := lipglossNone
	switch {
	case reErr.MatchString(s):
		base = sRed.Render
	case reWarn.MatchString(s):
		base = sYellow.Render
	}
	if p.search == "" {
		return base(s)
	}
	// highlight matches, case-insensitively
	lower, q := strings.ToLower(s), strings.ToLower(p.search)
	var b strings.Builder
	i := 0
	for {
		j := strings.Index(lower[i:], q)
		if j < 0 || len(lower) != len(s) {
			b.WriteString(base(s[i:]))
			break
		}
		b.WriteString(base(s[i : i+j]))
		b.WriteString(sMatch.Render(s[i+j : i+j+len(q)]))
		i += j + len(q)
	}
	return b.String()
}

func lipglossNone(s ...string) string { return strings.Join(s, " ") }
