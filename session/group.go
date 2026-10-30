package session

import (
	"regexp"
	"strings"
)

// Groups split the workers by kind of work: QA checks and development. Set by hand
// (g in the web, cs ctl group, cs ctl new --group) or guessed from the title/note.
const (
	GroupQA  = "qa"
	GroupDev = "dev"
)

var qaGuessRe = regexp.MustCompile(`(?i)(^|[^a-zа-яё])(qa|кьюэй)([^a-zа-яё]|$)|провер|ревью спек|приёмк`)

// GroupOf is the session's group: "main" for the conductor and the assistant, the
// one set by hand, or a guess (QA when the title or note says so, else development).
func (i *Instance) GroupOf() string {
	switch {
	case i.Conductor || i.Assistant:
		return "main"
	case i.Group != "":
		return i.Group // qa, dev or a group of the user's own
	case qaGuessRe.MatchString(i.Title) || qaGuessRe.MatchString(i.Note):
		return GroupQA
	}
	return GroupDev
}

// NormGroup turns user input into a group: qa, dev, a name of the user's own (up to
// 32 characters) or "" — guess again ("auto").
func NormGroup(s string) (string, bool) {
	t := strings.TrimSpace(s)
	switch strings.ToLower(t) {
	case "qa", "кьюэй", "проверка", "проверки", "qa-проверки":
		return GroupQA, true
	case "dev", "разработка", "develop":
		return GroupDev, true
	case "auto", "", "сам", "угадывать":
		return "", true
	}
	if r := []rune(t); len(r) > 32 || strings.ContainsAny(t, "\n\r\t") {
		return "", false
	}
	return t, true
}
