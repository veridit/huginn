// Package tui provides Huginn's human-facing ranked task browser.
package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/veridit/huginn/internal/task"
)

type Options struct {
	User   string
	Commit func(msg string) error
	// Take optionally owns the complete distributed take transaction, including commit.
	Take func(ref string) (task.Result, error)
}

func Run(ctx context.Context, store *task.Store, opts Options) error {
	if store == nil {
		return fmt.Errorf("task store is required")
	}
	m := newModel(store, opts)
	_, err := tea.NewProgram(m, tea.WithContext(ctx), tea.WithAltScreen()).Run()
	return err
}

type editorMsg struct{ err error }
type model struct {
	store                         *task.Store
	opts                          Options
	all, visible                  []task.Task
	findings                      []task.Finding
	docs                          map[string]*task.Document
	cursor, acceptance, offset    int
	width, height                 int
	detail, board, help           bool
	prompt, input, query, message string
	filter                        task.Filter
}

func newModel(s *task.Store, o Options) model {
	m := model{store: s, opts: o, width: 100, height: 30}
	m.refresh("")
	return m
}
func (m model) Init() tea.Cmd { return nil }
func (m *model) refresh(path string) {
	ts, err := m.store.Tasks()
	if err != nil {
		m.message = err.Error()
		return
	}
	task.Sort(ts)
	m.all = ts
	m.docs = map[string]*task.Document{}
	var docs []*task.Document
	for _, t := range ts {
		b, e := os.ReadFile(filepath.Join(m.store.Root, t.Path))
		if e != nil {
			m.message = e.Error()
			continue
		}
		d := task.Parse(t.Path, b)
		docs = append(docs, d)
		m.docs[t.Path] = d
	}
	m.findings = task.Lint(docs)
	m.applyFilter()
	for i, t := range m.visible {
		if t.Path == path {
			m.cursor = i
			break
		}
	}
	m.clamp()
}
func (m *model) clamp() {
	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if t, ok := m.selected(); ok && m.acceptance >= len(t.Acceptance) {
		m.acceptance = max(0, len(t.Acceptance)-1)
	}
}
func (m *model) applyFilter() {
	m.visible = nil
	for _, t := range m.all {
		if m.filter.Status != "" && t.Status != m.filter.Status || m.filter.Type != "" && t.Type != m.filter.Type || m.filter.Group != "" && t.Group != m.filter.Group {
			continue
		}
		if m.filter.Label != "" {
			found := false
			for _, l := range t.Labels {
				if l == m.filter.Label {
					found = true
				}
			}
			if !found {
				continue
			}
		}
		if m.query != "" && !strings.Contains(strings.ToLower(t.Slug+" "+t.Title+" "+t.ID), strings.ToLower(m.query)) {
			continue
		}
		m.visible = append(m.visible, t)
	}
	m.clamp()
}
func (m model) selected() (task.Task, bool) {
	if len(m.visible) == 0 {
		return task.Task{}, false
	}
	return m.visible[m.cursor], true
}
func (m *model) finish(r task.Result, err error, action string) {
	if err != nil {
		path := ""
		if t, ok := m.selected(); ok {
			path = t.Path
		}
		m.refresh(path)
		m.message = err.Error()
		return
	}
	m.message = action + " " + r.Task.ID
	if len(r.Warnings) > 0 {
		m.message += "\n" + strings.Join(r.Warnings, "\n")
	}
	if r.Changed && m.opts.Commit != nil && !(action == "take" && m.opts.Take != nil) {
		if e := m.opts.Commit("huginn: " + action + " " + r.Task.ID); e != nil {
			m.message = e.Error()
		}
	}
	m.refresh(r.Task.Path)
}
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case editorMsg:
		m.refresh("")
		if msg.err != nil {
			m.message = msg.err.Error()
		} else {
			m.message = "editor closed; re-linted"
			if m.opts.Commit != nil {
				if e := m.opts.Commit("huginn: edit task"); e != nil {
					m.message = e.Error()
				}
			}
		}
	case tea.KeyMsg:
		k := msg.String()
		if k == "ctrl+c" {
			return m, tea.Quit
		}
		if m.prompt != "" {
			switch k {
			case "esc":
				m.prompt = ""
				m.input = ""
			case "backspace":
				rs := []rune(m.input)
				if len(rs) > 0 {
					m.input = string(rs[:len(rs)-1])
				}
			case "enter":
				value := strings.TrimSpace(m.input)
				switch m.prompt {
				case "/":
					m.query = value
					m.applyFilter()
				case "new title":
					if value != "" {
						r, e := m.store.New(task.NewOptions{Title: value, Goal: value, Acceptance: []string{"Verify " + value}})
						m.finish(r, e, "new")
					}
				case "status":
					m.filter.Status = value
					m.applyFilter()
				case "type":
					m.filter.Type = value
					m.applyFilter()
				case "group":
					m.filter.Group = value
					m.applyFilter()
				case "label":
					m.filter.Label = value
					m.applyFilter()
				}
				m.prompt = ""
				m.input = ""
			default:
				if msg.Type == tea.KeyRunes || k == " " {
					m.input += string(msg.Runes)
					if k == " " && len(msg.Runes) == 0 {
						m.input += " "
					}
				}
			}
			return m, nil
		}
		t, ok := m.selected()
		switch k {
		case "q":
			return m, tea.Quit
		case "?":
			m.help = !m.help
		case "tab", "b":
			m.board = !m.board
		case "enter":
			m.detail = !m.detail
			m.offset = 0
		case "esc":
			m.detail = false
			m.help = false
		case "j", "down":
			if m.detail {
				m.acceptance++
				m.clamp()
			} else if m.cursor+1 < len(m.visible) {
				m.cursor++
			}
		case "k", "up":
			if m.detail {
				m.acceptance = max(0, m.acceptance-1)
			} else {
				m.cursor = max(0, m.cursor-1)
			}
		case "pgdown":
			m.offset += max(1, m.height/2)
		case "pgup":
			m.offset = max(0, m.offset-max(1, m.height/2))
		case "/":
			m.prompt = "/"
			m.input = m.query
		case "s":
			m.prompt = "status"
		case "y":
			m.prompt = "type"
		case "g":
			m.prompt = "group"
		case "l":
			m.prompt = "label"
		case "c":
			m.filter = task.Filter{}
			m.query = ""
			m.applyFilter()
		case "n":
			m.prompt = "new title"
		case "r":
			m.refresh(t.Path)
		case "t":
			if ok {
				var r task.Result
				var e error
				if m.opts.Take != nil {
					r, e = m.opts.Take(t.ID)
				} else {
					r, e = m.store.Take(t.ID, m.opts.User)
				}
				m.finish(r, e, "take")
			}
		case "d":
			if ok {
				r, e := m.store.Done(t.ID)
				m.finish(r, e, "done")
			}
		case "x":
			if ok {
				r, e := m.store.Drop(t.ID)
				m.finish(r, e, "drop")
			}
		case "J", "K":
			if ok {
				i := 0
				for i < len(m.all) && m.all[i].Path != t.Path {
					i++
				}
				o := task.MoveOptions{}
				if k == "J" && i+1 < len(m.all) {
					o.After = m.all[i+1].ID
				} else if k == "K" && i > 0 {
					o.Before = m.all[i-1].ID
				} else {
					break
				}
				r, e := m.store.Move(t.ID, o)
				m.finish(r, e, "move")
			}
		case " ":
			if ok && len(t.Acceptance) > 0 {
				n := m.acceptance + 1
				if !t.Acceptance[n-1].Checked {
					r, e := m.store.Check(t.ID, n)
					m.finish(r, e, "check")
				} else {
					d, e := m.store.Get(t.ID)
					if e == nil {
						lines := strings.Split(string(d.Bytes()), "\n")
						line := d.Task.Acceptance[n-1].Line - 1
						if line >= 0 && line < len(lines) {
							lines[line] = strings.Replace(strings.Replace(lines[line], "[x]", "[ ]", 1), "[X]", "[ ]", 1)
							e = task.AtomicWrite(filepath.Join(m.store.Root, d.Task.Path), []byte(strings.Join(lines, "\n")))
						}
					}
					m.finish(task.Result{Task: t, Changed: e == nil}, e, "uncheck")
				}
			}
		case "e":
			if ok {
				path, e := m.editorPath(t)
				if e != nil {
					m.message = e.Error()
					break
				}
				editor := os.Getenv("EDITOR")
				if editor == "" {
					editor = "vi"
				}
				args := strings.Fields(editor)
				if len(args) == 0 {
					m.message = "$EDITOR is empty"
					break
				}
				cmd := exec.Command(args[0], append(args[1:], path)...)
				return m, tea.ExecProcess(cmd, func(e error) tea.Msg { return editorMsg{e} })
			}
		}
	}
	m.clamp()
	return m, nil
}

// editorPath deliberately uses the selected path, not the possibly broken id.
// A malformed task must remain repairable, but cannot escape the task tree.
func (m model) editorPath(t task.Task) (string, error) {
	if t.Path == "" || filepath.IsAbs(t.Path) {
		return "", fmt.Errorf("invalid task path")
	}
	root, err := filepath.EvalSymlinks(m.store.Root)
	if err != nil {
		return "", err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, t.Path)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("task path escapes store")
	}
	actual, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if actual != path {
		return "", fmt.Errorf("symlinked task paths cannot be edited")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("task is not a regular file")
	}
	return path, nil
}

var dim = lipgloss.NewStyle().Faint(true)
var heading = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))

func safe(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, s)
}
func (m model) marker(path string) string {
	for _, f := range m.findings {
		if f.Path == path {
			return " !lint: " + safe(f.Problem)
		}
	}
	return ""
}
func (m model) row(t task.Task) string {
	pointer := "  "
	if selected, ok := m.selected(); ok && selected.Path == t.Path {
		pointer = "> "
	}
	return safe(fmt.Sprintf("%s%s  %s  [%s] %s %s", pointer, t.Slug, t.Title, t.Status, t.Type, t.Priority)) + "  " + dim.Render(safe(t.ID)) + m.marker(t.Path)
}
func (m model) detailText(t task.Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\nGoal\n%s\n\nAcceptance (j/k select, space toggle)\n", m.row(t), safe(t.Goal))
	for i, a := range t.Acceptance {
		box := " "
		if a.Checked {
			box = "x"
		}
		p := " "
		if i == m.acceptance {
			p = ">"
		}
		fmt.Fprintf(&b, "%s [%s] %d. %s\n", p, box, i+1, safe(a.Text))
	}
	b.WriteString("\nDependencies\n")
	for _, d := range task.DependencyDetails(t, m.all) {
		state := d.Status
		if d.Missing {
			state = "missing"
		}
		blocked := ""
		if state != "done" && state != "dropped" {
			blocked = " BLOCKED"
		}
		fmt.Fprintf(&b, "%s %s [%s]%s\n", safe(d.ID), safe(d.Title), safe(state), blocked)
	}
	if d := m.docs[t.Path]; d != nil {
		lines := strings.Split(string(d.Bytes()), "\n")
		in := false
		b.WriteString("\nNotes\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "## ") {
				in = strings.TrimSpace(line) == "## Notes"
				continue
			}
			if in {
				b.WriteString(safe(line) + "\n")
			}
		}
	}
	for _, f := range m.findings {
		if f.Path == t.Path {
			b.WriteString("\n!lint " + safe(f.String()))
		}
	}
	return b.String()
}
func (m model) View() string {
	title := "Huginn · ranked list"
	if m.board {
		title = "Huginn · board"
	}
	var content string
	switch {
	case m.help:
		content = "j/k navigate · enter detail · space toggle selected acceptance\nt take · d done (blocked until checks/deps complete) · x drop\nJ/K global move down/up · n new title · e $EDITOR then lint\ns status · y type · g group · l label · / search · c clear filters\nb/tab list/board · r refresh · PgUp/PgDown scroll · ? help · q quit\nEmpty filter input clears it. New tasks start with a goal and one unchecked verification item."
	case m.detail:
		if t, ok := m.selected(); ok {
			content = m.detailText(t)
		}
	case m.board:
		cols := []string{}
		for _, status := range task.Statuses {
			col := heading.Render(status) + "\n"
			for _, t := range m.visible {
				if t.Status == status {
					col += m.row(t) + "\n"
				}
			}
			cols = append(cols, lipgloss.NewStyle().Width(max(12, (m.width-6)/4)).MaxWidth(max(12, (m.width-6)/4)).Render(col))
		}
		content = lipgloss.JoinHorizontal(lipgloss.Top, cols...)
		for _, t := range m.visible {
			if !contains(task.Statuses, t.Status) {
				content += "\n" + m.row(t)
			}
		}
	default:
		for _, t := range m.visible {
			content += m.row(t) + "\n"
		}
		if len(m.visible) == 0 {
			content = "No tasks match. c clears filters; n creates a task."
		}
	}
	if m.detail {
		content = lipgloss.NewStyle().Width(max(1, m.width)).Render(content)
	}
	lines := strings.Split(content, "\n")
	height := max(3, m.height-8)
	start := min(m.offset, max(0, len(lines)-height))
	if !m.detail && !m.board && !m.help {
		start = max(start, m.cursor-height+1)
		start = min(start, m.cursor)
	}
	end := min(len(lines), start+height)
	footer := fmt.Sprintf("%d tasks · status=%s type=%s group=%s label=%s search=%s", len(m.visible), m.filter.Status, m.filter.Type, m.filter.Group, m.filter.Label, m.query)
	prompt := "j/k move · enter detail · b board · / search · ? help · q quit"
	if m.prompt != "" {
		prompt = m.prompt + "> " + safe(m.input)
	}
	return heading.Render(title) + "\n" + strings.Join(lines[start:end], "\n") + "\n" + dim.Render(safe(footer)) + "\n" + safe(m.message) + "\n" + prompt + "\n"
}
func contains(values []string, s string) bool {
	for _, v := range values {
		if v == s {
			return true
		}
	}
	return false
}
