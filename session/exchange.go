package session

import (
	"regexp"
	"strings"
)

// ConvLine is one line of the recent conversation shown in the quick reply.
type ConvLine struct {
	Text string
	User bool // a line of the user's own message
}

var noiseRes = []*regexp.Regexp{
	regexp.MustCompile(`^[*✻✶✳✢·※]?\s*\S+ for \d+(m \d+)?s( · .*)?$`), // "Baked for 23s · done 2:57 PM"
	regexp.MustCompile(`^(Ran|Thought for|Read|Searched for|Listed) .*$`),
	regexp.MustCompile(`^[*✻✶✳✢·]\s*\S+…`), // spinner lines "* Symbioting… (1m 9s)"
	regexp.MustCompile(`Update installed`),
	regexp.MustCompile(`^(new task\?|Tip:)`),
	regexp.MustCompile(`\(ctrl\+[a-z] to `),
	regexp.MustCompile(`^─+$`),
}

func isNoise(t string) bool {
	for _, re := range noiseRes {
		if re.MatchString(t) {
			return true
		}
	}
	return false
}

func isUserPromptLine(t string) bool {
	for _, p := range []string{"❯", ">"} {
		if strings.HasPrefix(t, p) {
			rest := strings.TrimPrefix(t, p)
			return rest == "" || rest[0] == ' ' || rest[0] == '\t'
		}
	}
	return false
}

// parseExchange extracts the last `turns` user messages and the agent's replies to
// them from a screen/scrollback capture, keeping at most maxLines lines.
func parseExchange(raw string, turns, maxLines int) []ConvLine {
	plain := strings.ReplaceAll(StripANSI(raw), "\u00a0", " ") // Claude Code uses NBSP after ❯
	lines := strings.Split(strings.TrimRight(plain, "\n"), "\n")
	// Drop Claude Code's input box and footer.
	if n := findInputLine(lines); n >= 0 {
		lines = lines[:n]
	}
	// Find the start of the last `turns` user messages.
	start := 0
	found := 0
	for i := len(lines) - 1; i >= 0; i-- {
		if isUserPromptLine(strings.TrimSpace(lines[i])) {
			found++
			start = i
			if found == turns {
				break
			}
		}
	}
	if found == 0 {
		start = len(lines) - maxLines
		if start < 0 {
			start = 0
		}
	}

	var out []ConvLine
	inUser := false
	blank := false
	for _, l := range lines[start:] {
		t := strings.TrimSpace(l)
		if t == "" {
			inUser = false
			if !blank && len(out) > 0 {
				out = append(out, ConvLine{})
			}
			blank = true
			continue
		}
		if isNoise(t) {
			continue
		}
		if isUserPromptLine(t) {
			inUser = true
			if len(out) > 0 && !blank {
				out = append(out, ConvLine{})
			}
		}
		out = append(out, ConvLine{Text: strings.TrimRight(l, " "), User: inUser})
		blank = false
	}
	for len(out) > 0 && out[len(out)-1].Text == "" {
		out = out[:len(out)-1]
	}
	if len(out) > maxLines {
		out = out[len(out)-maxLines:]
	}
	return out
}

// ParseExchange is parseExchange for callers outside the package.
func ParseExchange(raw string, turns, maxLines int) []ConvLine {
	return parseExchange(raw, turns, maxLines)
}

// ShowsApprovalPrompt reports whether a captured screen shows a permission prompt.
func ShowsApprovalPrompt(raw string) bool {
	plain := StripANSI(raw)
	for _, m := range needsInputMarkers {
		if strings.Contains(plain, m) {
			return true
		}
	}
	return false
}

// ExtractDraft returns unsent text in the agent's input box on a captured screen.
func ExtractDraft(raw string) string {
	return extractDraft(raw)
}

// InputBoxVisible reports whether Claude Code shows its input box (it has
// finished starting and accepts a message).
func InputBoxVisible(raw string) bool {
	return findInputLine(strings.Split(strings.TrimRight(raw, "\n"), "\n")) >= 0
}

// RecentExchange returns the last `turns` user messages and the agent's answers,
// read from the session's scrollback, plus the raw capture (for diagnostics).
func (i *Instance) RecentExchange(turns, maxLines int) ([]ConvLine, string) {
	if !i.started || i.tmuxSession == nil || i.Status == Paused {
		return nil, ""
	}
	content, err := i.tmuxSession.CapturePaneContentWithOptions("-3000", "-")
	if err != nil {
		return nil, ""
	}
	return parseExchange(content, turns, maxLines), content
}
