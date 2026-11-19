package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type fieldKind int

const (
	fieldText fieldKind = iota
	fieldChoice
	fieldChecks
)

// field is one form row: free text, a single choice or several checkboxes.
type field struct {
	key     string
	label   string
	kind    fieldKind
	input   textinput.Model
	options []string
	cursor  int
	checked map[int]bool
	hint    func(form) string
}

func textField(key, label, value, placeholder string) field {
	in := textinput.New()
	in.SetValue(value)
	in.Placeholder = placeholder
	in.CharLimit = 2000
	in.Width = 40
	return field{key: key, label: label, kind: fieldText, input: in}
}

func choiceField(key, label string, options []string, selected int) field {
	return field{key: key, label: label, kind: fieldChoice, options: options, cursor: selected}
}

func checksField(key, label string, options []string, on ...string) field {
	checked := map[int]bool{}
	for i, o := range options {
		for _, v := range on {
			if o == v {
				checked[i] = true
			}
		}
	}
	return field{key: key, label: label, kind: fieldChecks, options: options, checked: checked}
}

// form is a keyboard-driven form: tab moves, ←/→ choose, space toggles,
// ctrl+s submits, esc cancels.
type form struct {
	title  string
	fields []field
	focus  int
	err    string
	note   func(form) string
	submit func(form) tea.Cmd
}

func newForm(title string, fields []field, submit func(form) tea.Cmd) form {
	f := form{title: title, fields: fields, submit: submit}
	return f.withFocus(0)
}

func (f form) withFocus(i int) form {
	out := f
	out.fields = append([]field(nil), f.fields...)
	for j := range out.fields {
		if out.fields[j].kind == fieldText {
			if j == i {
				out.fields[j].input.Focus()
			} else {
				out.fields[j].input.Blur()
			}
		}
	}
	out.focus = i
	return out
}

func (f form) value(key string) string {
	for _, fl := range f.fields {
		if fl.key != key {
			continue
		}
		switch fl.kind {
		case fieldText:
			return strings.TrimSpace(fl.input.Value())
		case fieldChoice:
			return fl.options[fl.cursor]
		}
	}
	return ""
}

func (f form) values(key string) []string {
	out := []string{}
	for _, fl := range f.fields {
		if fl.key == key && fl.kind == fieldChecks {
			for i, o := range fl.options {
				if fl.checked[i] {
					out = append(out, o)
				}
			}
		}
	}
	return out
}

// update handles a key; done reports esc (cancel).
func (f form) update(msg tea.KeyMsg) (form, tea.Cmd, bool) {
	switch msg.String() {
	case "esc":
		return f, nil, true
	case "ctrl+s":
		out := f
		out.err = ""
		return out, f.submit(out), false
	case "tab", "down":
		return f.withFocus((f.focus + 1) % len(f.fields)), nil, false
	case "shift+tab", "up":
		return f.withFocus((f.focus - 1 + len(f.fields)) % len(f.fields)), nil, false
	}
	out := f.withFocus(f.focus)
	fl := &out.fields[out.focus]
	switch fl.kind {
	case fieldText:
		var cmd tea.Cmd
		fl.input, cmd = fl.input.Update(msg)
		return out, cmd, false
	case fieldChoice, fieldChecks:
		switch msg.String() {
		case "left", "h":
			fl.cursor = (fl.cursor - 1 + len(fl.options)) % len(fl.options)
		case "right", "l":
			fl.cursor = (fl.cursor + 1) % len(fl.options)
		case " ", "x":
			if fl.kind == fieldChecks {
				next := map[int]bool{}
				for k, v := range fl.checked {
					next[k] = v
				}
				next[fl.cursor] = !next[fl.cursor]
				fl.checked = next
			}
		}
	}
	return out, nil, false
}

func (f form) view(width int) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(f.title) + "\n\n")
	for i, fl := range f.fields {
		label := fl.label
		if i == f.focus {
			label = titleStyle.Render("› " + label)
		} else {
			label = "  " + label
		}
		b.WriteString(label + "\n    ")
		switch fl.kind {
		case fieldText:
			b.WriteString(fl.input.View())
		case fieldChoice:
			for j, o := range fl.options {
				if j == fl.cursor {
					b.WriteString(titleStyle.Render("("+"•"+") "+o) + "  ")
				} else {
					b.WriteString(mutedStyle.Render("( ) "+o) + "  ")
				}
			}
		case fieldChecks:
			for j, o := range fl.options {
				box := "[ ]"
				if fl.checked[j] {
					box = "[x]"
				}
				item := box + " " + o
				if i == f.focus && j == fl.cursor {
					item = selStyle.Render(item)
				}
				b.WriteString(item + "  ")
			}
		}
		b.WriteString("\n")
		if fl.hint != nil {
			if h := fl.hint(f); h != "" {
				b.WriteString("    " + mutedStyle.Render(h) + "\n")
			}
		}
	}
	if f.note != nil {
		if n := f.note(f); n != "" {
			b.WriteString("\n" + warnStyle.Width(width-4).Render(n) + "\n")
		}
	}
	if f.err != "" {
		b.WriteString("\n" + badStyle.Width(width-4).Render("error: "+f.err) + "\n")
	}
	return b.String()
}
