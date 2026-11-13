package app

import (
	"regexp"
	"strings"
)

// The board shows what a session waits for: when its last answer ends with a question
// («дорабатывать разбор, когда прогон дойдёт? 1… 2…»), that question — not the summary
// of the work — goes on its card, with the numbered options as buttons.

var qOptionRe = regexp.MustCompile(`^\s*(\d)[.)]\s+\S`)

// lastQuestion: the question the agent's last answer ends with, plus the numbered
// options right after it; "" when the last answer asks nothing or the user wrote since.
func lastQuestion(bs []tBlock) string {
	last := -1
	for i := len(bs) - 1; i >= 0; i-- {
		if bs[i].kind == "you" {
			return ""
		}
		if bs[i].kind == "ai" {
			last = i
			break
		}
	}
	if last < 0 {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(bs[last].text), "\n")
	// look only at the end of the answer: a question in the middle was already dealt with
	from := len(lines) - 14
	if from < 0 {
		from = 0
	}
	q := -1
	for i := len(lines) - 1; i >= from; i-- {
		t := strings.TrimSpace(lines[i])
		if strings.HasSuffix(t, "?") || strings.HasPrefix(strings.ToLower(strings.Trim(t, "*_ ")), "вопрос") {
			q = i
			break
		}
	}
	if q < 0 {
		return ""
	}
	// the question line itself, then its options (and the lines continuing them)
	out := []string{strings.TrimSpace(lines[q])}
	for _, l := range lines[q+1:] {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if !qOptionRe.MatchString(t) && len(out) == 1 {
			break // text after the question that is not an option: the answer moved on
		}
		if !qOptionRe.MatchString(t) && !strings.HasPrefix(l, " ") {
			break
		}
		out = append(out, t)
		if len(out) > 6 {
			break
		}
	}
	return clipRunes(strings.Join(out, "\n"), 700)
}
