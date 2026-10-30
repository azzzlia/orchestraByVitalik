package session

import (
	"regexp"
	"strings"
)

var editAskRe = regexp.MustCompile(`Do you want to (make this edit to|create|overwrite) (.+?)\?`)

// PermissionRequest reads what a permission prompt on the screen asks for:
// "git push -u origin im/x" for a Bash command, "правка app.go" for an edit.
// Empty when the screen shows no permission prompt.
func PermissionRequest(raw string) string {
	plain := strings.ReplaceAll(StripANSI(raw), " ", " ")
	lines := strings.Split(strings.TrimRight(plain, "\n "), "\n")
	if len(lines) > 45 {
		lines = lines[len(lines)-45:]
	}
	ask := -1
	for n := len(lines) - 1; n >= 0; n-- {
		t := strings.TrimSpace(lines[n])
		if m := editAskRe.FindStringSubmatch(t); m != nil {
			switch m[1] {
			case "create":
				return "создать " + m[2]
			case "overwrite":
				return "перезаписать " + m[2]
			}
			return "правка " + m[2]
		}
		if strings.Contains(t, "Do you want to proceed?") {
			ask = n
			break
		}
	}
	if ask < 0 {
		return ""
	}
	// The command sits between two dashed rulers (╌╌╌) above the question.
	var cmd []string
	inBox := false
	for n := ask - 1; n >= 0 && n >= ask-25; n-- {
		t := strings.TrimSpace(lines[n])
		if t != "" && strings.Trim(t, "╌") == "" {
			if inBox {
				break
			}
			inBox = true
			continue
		}
		if inBox && t != "" {
			cmd = append([]string{t}, cmd...)
		}
	}
	if len(cmd) == 0 {
		// Older layout: the line right under the tool header ("Bash command").
		for n := ask - 1; n >= 0 && n >= ask-25; n-- {
			if strings.HasSuffix(strings.TrimSpace(lines[n]), "command") && n+1 < ask {
				return strings.TrimSpace(lines[n+1])
			}
		}
		return "?"
	}
	s := strings.Join(cmd, " ")
	if r := []rune(s); len(r) > 160 {
		s = string(r[:160]) + "…"
	}
	return s
}
