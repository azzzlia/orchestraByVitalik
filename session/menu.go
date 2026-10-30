package session

import (
	"regexp"
	"strconv"
	"strings"
)

// ChoiceMenu is a question Claude Code shows with numbered options (the
// AskUserQuestion tool): "❯ 1. … (Recommended) / 2. … / Type something / Chat about this".
// It waits for a key press, so to Squad it looks like an idle session.
type ChoiceMenu struct {
	Question string       `json:"question"`
	Tabs     []string     `json:"tabs,omitempty"` // headers of several questions asked at once
	Options  []MenuOption `json:"options"`
	Cursor   int          `json:"cursor,omitempty"` // number of the highlighted option
	Multi    bool         `json:"multi,omitempty"`  // several options can be ticked
	Review   bool         `json:"review,omitempty"` // "Review your answers / Submit answers"
}

// MenuOption is one numbered line of a ChoiceMenu.
type MenuOption struct {
	Number      int    `json:"number"`
	Label       string `json:"label"`
	Recommended bool   `json:"recommended,omitempty"`
	Checked     bool   `json:"checked,omitempty"`
	Free        bool   `json:"free,omitempty"` // "Type something": an answer in your own words
	Chat        bool   `json:"chat,omitempty"` // "Chat about this"
	Desc        string `json:"desc,omitempty"` // the grey description lines under the option
}

var menuLineRe = regexp.MustCompile(`^\s*(❯\s*)?(\d+)\.\s+(?:\[([ ✔✓x])\]\s*)?(.*?)\s*$`)

// ParseChoiceMenu finds a choice menu at the bottom of a captured screen.
// Permission prompts ("Do you want to proceed?") are not menus to answer.
func ParseChoiceMenu(raw string) (ChoiceMenu, bool) {
	var m ChoiceMenu
	plain := strings.ReplaceAll(StripANSI(raw), " ", " ")
	lines := strings.Split(strings.TrimRight(plain, "\n "), "\n")
	start := len(lines) - 45
	if start < 0 {
		start = 0
	}
	lines = lines[start:]
	text := strings.Join(lines, "\n")
	// AskUserQuestion always shows its header chip(s) ("☐ Fruit"); a permission prompt
	// never does. Without a chip, a permission marker means it is not a question.
	// A TodoWrite list draws the same boxes ("⎿ ☐ Write tests", deeper indented items),
	// so only a tab bar counts: near the left edge, not under ⎿. Any permission marker
	// below the tab bar still means it is a permission prompt, not a question.
	chipAt := -1
	for n, l := range lines {
		if isTabBar(l) {
			chipAt = n
		}
	}
	markers := needsInputMarkers
	below := text
	if chipAt >= 0 {
		// A question may say "Do you want to create…"; a permission prompt always
		// has its "No, and tell Claude…" option or "Do you want to proceed?".
		below = strings.Join(lines[chipAt+1:], "\n")
		markers = []string{"No, and tell Claude what to do differently", "Do you want to proceed?"}
	}
	for _, mk := range markers {
		if strings.Contains(below, mk) {
			return m, false
		}
	}
	review := strings.Contains(text, "Ready to submit your answers?")
	footer := false
	for n := len(lines) - 1; n >= 0 && n >= len(lines)-6; n-- {
		if strings.Contains(lines[n], "Enter to select") && strings.Contains(lines[n], "Esc to cancel") {
			footer = true
			break
		}
	}
	if !footer && !review {
		return m, false
	}
	first := -1
	for n, l := range lines {
		t := strings.TrimSpace(l)
		if isTabBar(l) {
			m.Tabs = parseTabs(t)
			m.Options, first = nil, -1 // a new question starts below the tab bar
			continue
		}
		g := menuLineRe.FindStringSubmatch(l)
		if g == nil {
			// An indented line right under an option is its description.
			if len(m.Options) > 0 && strings.HasPrefix(l, "     ") && t != "" && strings.Trim(t, "─") != "" {
				o := &m.Options[len(m.Options)-1]
				o.Desc = strings.TrimSpace(o.Desc + " " + t)
			}
			continue
		}
		num, _ := strconv.Atoi(g[2])
		if num != len(m.Options)+1 {
			// Numbered text from the conversation above the menu: start over.
			if num != 1 {
				continue
			}
			m.Options, first = nil, -1
		}
		if first < 0 {
			first = n
		}
		o := MenuOption{Number: num, Label: g[4], Checked: g[3] != "" && g[3] != " "}
		o.Recommended = strings.HasSuffix(o.Label, "(Recommended)")
		low := strings.ToLower(o.Label)
		o.Free = strings.HasPrefix(low, "type something")
		o.Chat = strings.HasPrefix(low, "chat about this")
		if g[3] != "" {
			m.Multi = true
		}
		if g[1] != "" {
			m.Cursor = num
		}
		m.Options = append(m.Options, o)
	}
	if len(m.Options) < 2 || first < 0 {
		return m, false
	}
	// The question is the block of text right above the first option; Claude Code
	// may draw it with a "│" bar on the left and wrap it over several lines.
	var q []string
	for n := first - 1; n >= 0 && n >= first-12; n-- {
		t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[n]), "│"))
		if isTabBar(lines[n]) || (t != "" && strings.Trim(t, "─") == "") {
			break
		}
		if t == "" {
			if len(q) > 0 {
				break
			}
			continue
		}
		q = append([]string{t}, q...)
	}
	m.Question = strings.Join(q, " ")
	if r := []rune(m.Question); len(r) > 400 {
		m.Question = string(r[:400]) + "…"
	}
	m.Review = review
	if review {
		m.Question = "Ready to submit your answers?"
	}
	return m, true
}

// isTabBar: the AskUserQuestion header line ("☐ Fruit", "←  ☒ A  ☐ B  ✔ Submit  →"),
// not a todo list item.
func isTabBar(l string) bool {
	if strings.Contains(l, "⎿") {
		return false
	}
	t := strings.TrimLeft(l, " ")
	if len(l)-len(t) > 2 {
		return false
	}
	t = strings.TrimSpace(strings.TrimPrefix(t, "←"))
	return strings.HasPrefix(t, "☐") || strings.HasPrefix(t, "☒") || strings.HasPrefix(t, "✔ Submit")
}

func parseTabs(bar string) []string {
	var tabs []string
	for _, part := range strings.FieldsFunc(bar, func(r rune) bool { return r == '☐' || r == '☒' }) {
		p := strings.TrimSpace(strings.Trim(strings.TrimSpace(part), "←→"))
		p = strings.TrimSpace(strings.TrimSuffix(p, "✔ Submit"))
		if p != "" {
			tabs = append(tabs, p)
		}
	}
	return tabs
}

// MenuKey identifies one question of a menu (question and labels).
func MenuKey(m ChoiceMenu) string {
	k := m.Question
	for _, o := range m.Options {
		k += "|" + o.Label
	}
	return k
}

// Recommended returns the option marked "(Recommended)", if any.
func (m ChoiceMenu) Recommended() (MenuOption, bool) {
	for _, o := range m.Options {
		if o.Recommended {
			return o, true
		}
	}
	return MenuOption{}, false
}

// Option returns the option with this number.
func (m ChoiceMenu) Option(n int) (MenuOption, bool) {
	for _, o := range m.Options {
		if o.Number == n {
			return o, true
		}
	}
	return MenuOption{}, false
}

// FreeOption returns "Type something", if the menu has it.
func (m ChoiceMenu) FreeOption() (MenuOption, bool) {
	for _, o := range m.Options {
		if o.Free {
			return o, true
		}
	}
	return MenuOption{}, false
}

// Summary is the menu in one line: "Пушить? — 1. Да (Recommended) · 2. Нет".
func (m ChoiceMenu) Summary() string {
	var opts []string
	for _, o := range m.Options {
		if o.Chat {
			continue
		}
		l := o.Label
		if o.Checked {
			l = "[✔] " + l
		}
		opts = append(opts, strconv.Itoa(o.Number)+". "+l)
	}
	s := strings.Join(opts, " · ")
	if m.Question != "" {
		s = m.Question + " — " + s
	}
	if m.Multi {
		s += " (можно несколько)"
	}
	return s
}

// userOnlyWords: an option about these is picked by the conductor only after the
// user's «да» (cs ctl answer --user-ok), and Squad never picks it by itself.
var userOnlyWords = []string{"push", "пуш", "merge", "мерж", "мёрж", "влить", "влей", "вольём", "слить", "слей", "залить", "залей",
	"deploy", "деплой", "выкат", "выложить", "релиз", "release", "delete", "удал", "стереть", "сотри", "drop", "force",
	"reset", "сброс", "rebase", "revert", "discard", "overwrite", "перезапис", "отправ", "опубликов", "publish", "send",
	"pull request", "чат", "комментар", "comment", "оплат", "prod", "прод", "approve", "одобр", "bypass", "закрыть задачу",
	"upload", "загрузить на"}

// autoOnlyWords: Squad does not auto-pick these either (local but lasting changes).
var autoOnlyWords = []string{"commit", "коммит", "закоммит", "close", "закрыть", "migrat", "миграц", "install", "установ"}

// wordRe: short words matched as whole words ("mr" but not "summary").
var wordRe = regexp.MustCompile(`(?i)(^|[^a-zа-яё])(mr|pr|rm|post)([^a-zа-яё]|$)`)

func containsAny(low string, words []string) bool {
	for _, w := range words {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}

// IsRisky reports whether text mentions an action that must not be taken without the user.
func IsRisky(text string) bool {
	low := " " + strings.ToLower(text) + " "
	return containsAny(low, userOnlyWords) || wordRe.MatchString(low)
}

func (m ChoiceMenu) text(o *MenuOption) string {
	t := m.Question + " " + strings.Join(m.Tabs, " ")
	if o != nil {
		return t + " " + o.Label + " " + o.Desc
	}
	for _, x := range m.Options {
		t += " " + x.Label + " " + x.Desc
	}
	return t
}

// Risky reports whether Squad must not answer this menu by itself: anything about
// push/merge/deletion/sending and also commits or installs, in the question, the
// labels or the descriptions under them.
func (m ChoiceMenu) Risky() bool {
	all := m.text(nil)
	return IsRisky(all) || containsAny(strings.ToLower(all), autoOnlyWords)
}

// negationStarts: an option that begins like this is a "no" ("Пока не пушить").
var negationStarts = []string{"не ", "нет", "no", "not ", "don't", "don’t", "do not", "skip", "пропуст", "cancel", "отмен", "позже",
	"later", "пока не", "подожд", "wait", "оставить как есть", "ничего не"}

// isPlainNo: the label starts with a negation as a whole word ("No", not "Now"/"Normal")
// and does nothing else ("Skip CI and force push", "Отменить ревью и удалить ветку" are not).
func isPlainNo(label string) bool {
	low := strings.ToLower(strings.TrimSpace(label))
	neg := false
	for _, w := range negationStarts {
		if !strings.HasPrefix(low, w) {
			continue
		}
		rest := []rune(low[len(w):])
		if strings.HasSuffix(w, " ") || len(rest) == 0 || !isLetter(rest[0]) || negationStems[w] {
			neg = true
			break
		}
	}
	if !neg {
		return false
	}
	for _, c := range []string{" and ", " then ", " и ", " потом ", " затем ", " а ", ";", " + ", " & ", "→", " but ", " но "} {
		if strings.Contains(low, c) {
			return false
		}
	}
	return true
}

// negationStems are word beginnings ("пропустить", "skipping"), not whole words.
var negationStems = map[string]bool{"пропуст": true, "отмен": true, "подожд": true, "skip": true, "cancel": true, "wait": true}

func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'а' && r <= 'я') || r == 'ё' || r == '\''
}

// RiskyChoice reports whether picking this option needs the user's «да»: the
// question, the option or its description is about push/merge/deletion and the
// option is not a plain "no" (its label starts with a negation).
func (m ChoiceMenu) RiskyChoice(o MenuOption) bool {
	if isPlainNo(o.Label) {
		return false
	}
	return IsRisky(m.text(&o))
}

// PermissionLike: a dialog that grants permissions (bypass, auto mode, "don't ask
// again", "Would you like to proceed?") — only the user answers it, it is not a menu.
func (m ChoiceMenu) PermissionLike() bool {
	q := strings.ToLower(m.Question)
	if strings.Contains(q, "proceed") {
		return true
	}
	for _, o := range m.Options {
		l := strings.ToLower(o.Label)
		if containsAny(l, []string{"bypass", "auto-accept", "auto accept", "don't ask again", "don’t ask again", "auto mode", "switch to",
			"allow all", "during this session", "always allow", "for this session", "no, and tell claude", "accept edits", "permission"}) {
			return true
		}
	}
	return false
}
