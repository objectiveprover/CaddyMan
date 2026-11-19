package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"caddyman/internal/caddyfile"
	"caddyman/internal/config"
	"caddyman/internal/services"
)

type field struct {
	id      string
	label   string
	help    string
	ti      textinput.Model
	choices []string
	choice  int
	// labels, when set, are shown instead of the choices, as they are,
	// one at a time (the Service field: unit names, maybe many).
	labels []string
}

func (f *field) value() string {
	if f.choices != nil {
		return f.choices[f.choice]
	}
	return strings.TrimSpace(f.ti.Value())
}

func (f *field) setChoice(v string) {
	for i, c := range f.choices {
		if c == v {
			f.choice = i
			return
		}
	}
}

func (f *field) setBool(on bool) { f.setChoice(map[bool]string{true: "yes", false: "no"}[on]) }

// Values of the Serves choice.
const (
	servesProxy  = "proxy"
	servesStatic = "static files"
)

// form is the Add Site and Edit Site view.
type form struct {
	fields []*field
	focus  int
	offset int // first visible line of the (scrolling) field list
	err    string
	title  string // view name in the top bar
	orig   string // address of the site being edited; "" for a new one

	// initial holds the fields' values when the form opened, auto the ones
	// last filled in for a picked service: a field still holding either
	// may be filled in again, one changed by hand is kept.
	initial, auto map[string]string
}

// noService is the Service field's first choice.
const noService = ""

// serviceField offers ServiceMan's tracked services, plus the site's own
// service when ServiceMan doesn't track it.
func serviceField(tracked []services.Unit, s *config.Site) *field {
	f := &field{id: "service", label: "Service",
		help:    "The systemd service running the app (from ServiceMan's tracked services)",
		choices: []string{noService}, labels: []string{"None"}}
	for _, u := range tracked {
		f.choices = append(f.choices, u.Key())
		f.labels = append(f.labels, u.Label())
	}
	if s != nil && s.Service != "" {
		u := services.Unit{Name: s.Service, Scope: s.ServiceScope}
		f.setChoice(u.Key())
		if f.value() != u.Key() {
			f.choices = append(f.choices, u.Key())
			f.labels = append(f.labels, u.Label()+" — not tracked by ServiceMan")
			f.choice = len(f.choices) - 1
		}
	}
	if len(tracked) == 0 {
		f.help = "No services tracked in ServiceMan yet (track them there to pick one here)"
	}
	return f
}

func newFields(c *config.Config) []*field {
	text := func(id, label, help string) *field {
		return &field{id: id, label: label, help: help, ti: newTextInput()}
	}
	choice := func(id, label, help string, choices ...string) *field {
		return &field{id: id, label: label, help: help, choices: choices}
	}
	return []*field{
		text("name", "Name", ""), // help: the address, see fieldLines
		choice("serves", "Serves", "", servesProxy, servesStatic),
		text("upstream", "Upstream", "A port on this machine, or host:port"),
		text("root", "Directory", "The directory to serve (Caddy's user must be able to read it)"),
		choice("https", "HTTPS", "Certificate from Caddy's own CA (devices must trust its root certificate)", "yes", "no"),
		choice("trusted", "Trusted only", "Imports ("+c.TrustedSnippet+") from your Caddyfile: only trusted clients get in", "yes", "no"),
		text("redirect", "Redirect / to", "Optional: send requests for / there, e.g. /web/"),
		choice("enabled", "Enabled", "A disabled site stays in the list but not in Caddy's config", "yes", "no"),
	}
}

// openForm opens Add Site (s nil) or Edit Site for s.
func (m *Model) openForm(s *config.Site) tea.Cmd {
	f := &form{fields: newFields(m.cfg), title: "Add Site"}
	// Service goes after Upstream and Directory.
	svc := serviceField(m.tracked, s)
	at := 4
	f.fields = append(f.fields[:at], append([]*field{svc}, f.fields[at:]...)...)
	if s != nil {
		f.title, f.orig = "Edit Site", m.cfg.Address(*s)
		f.get("name").ti.SetValue(s.Name)
		if s.Kind == config.Static {
			f.get("serves").setChoice(servesStatic)
		}
		f.get("upstream").ti.SetValue(s.Upstream)
		f.get("root").ti.SetValue(s.Root)
		f.get("https").setBool(s.HTTPS)
		f.get("trusted").setBool(s.TrustedOnly)
		f.get("redirect").ti.SetValue(s.RedirectRoot)
		f.get("enabled").setBool(s.Enabled)
	}
	m.form, m.mode = f, modeForm
	f.setFocus(0)
	f.initial, f.auto = map[string]string{}, map[string]string{}
	for _, fl := range f.fields {
		f.initial[fl.id] = fl.value()
	}
	return nil
}

// replaceable reports whether field id may be filled in for a picked
// service: it's as the form opened or as last filled in.
func (f *form) replaceable(id string) bool {
	v := f.val(id)
	a, filled := f.auto[id]
	return v == f.initial[id] || (filled && v == a)
}

// fill sets field id for a picked service, remembering the value.
func (f *form) fill(id, v string) {
	fl := f.get(id)
	if fl.choices != nil {
		fl.setChoice(v)
	} else {
		fl.ti.SetValue(v)
	}
	f.auto[id] = fl.value()
}

func (f *form) get(id string) *field {
	for _, fl := range f.fields {
		if fl.id == id {
			return fl
		}
	}
	return nil
}

func (f *form) val(id string) string {
	if fl := f.get(id); fl != nil {
		return fl.value()
	}
	return ""
}

// site is the site as the form describes it.
func (f *form) site() config.Site {
	s := config.Site{
		Name:         strings.ToLower(f.val("name")),
		Enabled:      f.val("enabled") == "yes",
		Kind:         config.Proxy,
		HTTPS:        f.val("https") == "yes",
		TrustedOnly:  f.val("trusted") == "yes",
		RedirectRoot: f.val("redirect"),
	}
	if scope, unit, ok := strings.Cut(f.val("service"), "/"); ok {
		s.Service, s.ServiceScope = unit, scope
	}
	if f.val("serves") == servesStatic {
		s.Kind, s.Root = config.Static, f.val("root")
	} else {
		s.Upstream = config.NormalizeUpstream(f.val("upstream"))
	}
	return s
}

// hidden reports whether field i doesn't apply: Upstream for static files,
// Directory for a proxy.
func (f *form) hidden(i int) bool {
	switch f.fields[i].id {
	case "upstream":
		return f.val("serves") == servesStatic
	case "root":
		return f.val("serves") != servesStatic
	}
	return false
}

// move focuses the next shown field in direction d, stopping at the first
// and last fields (no wrapping around).
func (f *form) move(d int) {
	for i := f.focus + d; i >= 0 && i < len(f.fields); i += d {
		if !f.hidden(i) {
			f.setFocus(i)
			return
		}
	}
}

func (f *form) setFocus(i int) {
	f.focus = i
	for j, fl := range f.fields {
		if fl.choices == nil {
			if j == f.focus {
				fl.ti.Focus()
			} else {
				fl.ti.Blur()
				fl.ti.CursorStart()
			}
		}
	}
}

func (f *form) key(m *Model, msg tea.KeyMsg) tea.Cmd {
	cur := f.fields[f.focus]
	switch k := msg.String(); k {
	case "ctrl+g", "esc":
		m.form, m.mode = nil, modeList
		m.setMsg("Cancelled")
		return nil
	case "tab", "ctrl+n", "down":
		f.move(1)
		return nil
	case "shift+tab", "ctrl+p", "up":
		f.move(-1)
		return nil
	case "enter":
		if cur.choices != nil {
			cur.choice = (cur.choice + 1) % len(cur.choices)
			return f.choiceChanged(m, cur)
		} else {
			f.move(1)
		}
		return nil
	}
	if cur.choices != nil {
		switch msg.String() {
		case " ", "ctrl+f", "right":
			cur.choice = (cur.choice + 1) % len(cur.choices)
		case "ctrl+b", "left":
			cur.choice = (cur.choice - 1 + len(cur.choices)) % len(cur.choices)
		default:
			return nil
		}
		return f.choiceChanged(m, cur)
	}
	var cmd tea.Cmd
	cur.ti, cmd = cur.ti.Update(msg)
	return cmd
}

// choiceChanged follows a new choice: picking a service fills in Upstream
// from its health URL in ServiceMan (when Upstream is empty or was filled in
// for another service), then asks the service's binary for service-info.
func (f *form) choiceChanged(m *Model, fl *field) tea.Cmd {
	if fl.id != "service" {
		return nil
	}
	for _, u := range m.tracked {
		if u.Key() != fl.value() {
			continue
		}
		// An edited site's Upstream is only filled in when it's empty.
		free := f.val("upstream") == "" || (f.orig == "" && f.replaceable("upstream"))
		if up := services.Upstream(u.URL); up != "" && free {
			f.fill("upstream", up)
		}
		return queryServiceInfo(f, u)
	}
	return nil
}

// prefixKey handles C-c chords.
func (f *form) prefixKey(m *Model, combo string) (tea.Cmd, bool) {
	switch combo {
	case "ctrl+c ctrl+c":
		return f.submit(m), true
	case "ctrl+c ctrl+p":
		m.showPreview = !m.showPreview
		return nil, true
	}
	return nil, false
}

func (f *form) submit(m *Model) tea.Cmd {
	s := f.site()
	if err := m.cfg.Validate(f.orig, s); err != nil {
		f.err = err.Error()
		m.setErr("%s", f.err)
		return nil
	}
	f.err = ""
	orig, addr := f.orig, m.cfg.Address(s)
	desc := "Add " + addr
	if orig != "" {
		desc = "Save " + addr
	}
	return m.saveChange(desc, func(c *config.Config) error {
		if err := c.Validate(orig, s); err != nil {
			return err
		}
		c.Put(orig, s)
		return nil
	}, f, func(m *Model) {
		m.form, m.mode, m.filter = nil, modeList, ""
		m.selectAddr(addr)
	})
}

// fieldLines renders the shown fields for a column w wide: per field a label
// line (name and gray description), the input below it (after a ❯ for text
// fields, a └ for choices) and a ─ line under it, except under the last
// field. start and end are the lines to keep on screen: the focused field.
func (f *form) fieldLines(m *Model, w int) (lines []string, start, end int) {
	first, last := -1, -1
	for i := range f.fields {
		if !f.hidden(i) {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	for i, fl := range f.fields {
		if f.hidden(i) {
			continue
		}
		focused := i == f.focus
		bottom := i != last
		label := sBold.Render(fl.label)
		if focused {
			label = sHighlight.Render(fl.label)
			start = len(lines)
			if i == first {
				start = 0
			}
		}
		help := fl.help
		if fl.id == "name" {
			help = "A short name, or a full address with dots"
			if s := f.site(); s.Name != "" {
				help = "Served at " + s.URL(m.cfg.DomainSuffix)
			}
		}

		if fl.choices == nil {
			if tw := inputTextWidth(w); fl.ti.Width != tw {
				fl.ti.Width = tw
				// Recompute the visible window: a value set while Width was 0
				// would otherwise render in full and wrap.
				pos := fl.ti.Position()
				fl.ti.CursorEnd()
				fl.ti.SetCursor(pos)
			}
			tf := textField(cursorMark(focused), label, help, fl.ti.View(), focused, w)
			if !bottom {
				tf = tf[:2]
			}
			lines = append(lines, tf...)
		} else {
			head := cursorMark(focused) + label
			if help != "" {
				head += "  " + sDesc.Render(help)
			}
			// The chosen option is bold green, like its ✓; the others keep a
			// blank in the ✓'s place so labels don't shift.
			var opts []string
			if fl.labels != nil {
				// One at a time: there may be many.
				opts = append(opts, sChosen.Render(fl.labels[fl.choice])+" "+sGreen.Render("✓"))
				if len(fl.labels) > 1 {
					opts = append(opts, sDim.Render(fmt.Sprintf("%d of %d (SPC / C-b for the others)", fl.choice+1, len(fl.labels))))
				}
			} else {
				for j, c := range fl.choices {
					c = choiceLabel(c)
					if j != fl.choice {
						opts = append(opts, sDim.Render(c)+"  ")
					} else {
						opts = append(opts, sChosen.Render(c)+" "+sGreen.Render("✓"))
					}
				}
			}
			lines = append(lines, trunc(head, w), trunc(gutter+inputLead(focused)+strings.Join(opts, "   "), w))
			if bottom {
				lines = append(lines, fieldBottom(focused, w))
			}
		}
		if focused {
			end = len(lines)
		}
	}
	if f.focus == last {
		end = len(lines)
	}
	return lines, start, end
}

// scroll returns the h lines of the field list to show, keeping lines start
// to end (the focused field) visible; ↑/↓ markers show there is more.
func (f *form) scroll(lines []string, start, end, h int) []string {
	if len(lines) <= h {
		f.offset = 0
		return lines
	}
	margin := 1 // room for the ↑/↓ markers
	if h < 5 {
		margin = 0
	}
	if f.offset > start-margin {
		f.offset = start - margin
	}
	if bottom := end + margin; bottom > f.offset+h {
		f.offset = bottom - h
	}
	f.offset = max(0, min(f.offset, len(lines)-h))
	out := append([]string(nil), lines[f.offset:f.offset+h]...)
	if margin > 0 {
		if f.offset > 0 {
			out[0] = gutter + sDim.Render("↑ more")
		}
		if f.offset+h < len(lines) {
			out[h-1] = gutter + sDim.Render("↓ more")
		}
	}
	return out
}

// view is the form screen: the fields, and the Caddyfile preview when C-c
// C-p turned it on. Wide screens show the preview beside the fields, narrow
// ones below them, in up to half the height.
func (f *form) view(m *Model, w, h int) string {
	var ctx []string
	if f.orig != "" {
		ctx = append(ctx, sDim.Render(f.orig))
	}
	right := onOff("Preview", "C-c C-p", m.showPreview) + "    " + m.caddyCell()
	out := []string{topBar(w, f.title, ctx, right)}
	h -= 2
	if f.err != "" {
		out = append(out, trunc(gutter+sRed.Render(f.err), w))
		h--
	}
	h = max(1, h)

	var preview []string
	if m.showPreview {
		preview = f.previewLines(m)
	}
	switch {
	case preview == nil || w < 110 && h/2 < 4:
		lines, start, end := f.fieldLines(m, w-len(colGap))
		return strings.Join(append(out, f.scroll(lines, start, end, h)...), "\n")
	case w < 110:
		ph := min(len(preview)+1, h/2)
		lines, start, end := f.fieldLines(m, w-len(colGap))
		fields := f.scroll(lines, start, end, h-ph)
		for len(fields) < h-ph {
			fields = append(fields, "")
		}
		out = append(out, fields...)
		out = append(out, "")
		for _, l := range preview[:ph-1] {
			out = append(out, trunc(gutter+l, w))
		}
		return strings.Join(out, "\n")
	}
	colW := w / 2
	lines, start, end := f.fieldLines(m, colW-len(colGap))
	left := strings.Join(f.scroll(lines, start, end, h), "\n")
	if len(preview) > h {
		preview = preview[:h]
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(colW).MaxWidth(colW).Render(left),
		lipgloss.NewStyle().MaxWidth(w-colW).Render(strings.Join(preview, "\n")))
	return strings.Join(append(out, body), "\n")
}

// previewLines is the site's file as it would be written, under a header.
func (f *form) previewLines(m *Model) []string {
	s := f.site()
	head := "Preview: " + config.Tilde(m.cfg.LocalFile(s))
	if !s.Enabled {
		head = "Preview (disabled: no file, left out of Caddy's config)"
	}
	preview := []string{sHeader.Render(head), ""}
	for _, l := range strings.Split(strings.TrimRight(caddyfile.Site(m.cfg, s), "\n"), "\n") {
		preview = append(preview, strings.ReplaceAll(l, "\t", "    "))
	}
	return preview
}
