package app

import (
	"bufio"
	"encoding/json"
	"html"
	"os"
	"path/filepath"
	"strings"
)

// The web view's history comes from the session's Claude Code transcript
// (~/.claude/projects/*/<conversation>.jsonl): the whole conversation, even when the
// terminal's scrollback is short or was cleared by a redraw.

// transcriptPath finds the transcript file of a conversation, "" when there is none.
func transcriptPath(conv string) string {
	if conv == "" || strings.ContainsAny(conv, `/\.`) {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	m, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", conv+".jsonl"))
	if len(m) == 0 {
		return ""
	}
	best, bestT := m[0], int64(0)
	for _, p := range m {
		if st, err := os.Stat(p); err == nil && st.ModTime().UnixNano() > bestT {
			best, bestT = p, st.ModTime().UnixNano()
		}
	}
	return best
}

type tLine struct {
	cls, text string
}

// transcriptLines renders a transcript as readable lines: the user's messages, the
// agent's answers, its tool calls (one line each) and the start of tool results.
func transcriptLines(path string) []tLine {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []tLine
	add := func(cls, prefix, text string, maxLines int) {
		text = strings.TrimRight(strings.ReplaceAll(text, "\r", ""), "\n ")
		if strings.TrimSpace(text) == "" {
			return
		}
		ls := strings.Split(text, "\n")
		cut := false
		if maxLines > 0 && len(ls) > maxLines {
			ls, cut = ls[:maxLines], true
		}
		for i, l := range ls {
			p := strings.Repeat(" ", len([]rune(prefix)))
			if i == 0 {
				p = prefix
			}
			out = append(out, tLine{cls, p + l})
		}
		if cut {
			out = append(out, tLine{"dim", strings.Repeat(" ", len([]rune(prefix))) + "…"})
		}
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 32<<20)
	for sc.Scan() {
		var e struct {
			Type    string `json:"type"`
			IsMeta  bool   `json:"isMeta"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.IsMeta || (e.Type != "user" && e.Type != "assistant") {
			continue
		}
		var s string
		if json.Unmarshal(e.Message.Content, &s) == nil {
			if e.Type == "user" && !strings.HasPrefix(strings.TrimSpace(s), "<") {
				out = append(out, tLine{"", ""})
				add("you", "❯ ", s, 0)
			}
			continue
		}
		var items []struct {
			Type    string          `json:"type"`
			Text    string          `json:"text"`
			Name    string          `json:"name"`
			Input   json.RawMessage `json:"input"`
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(e.Message.Content, &items) != nil {
			continue
		}
		for _, it := range items {
			switch it.Type {
			case "text":
				if e.Type == "user" {
					if !strings.HasPrefix(strings.TrimSpace(it.Text), "<") {
						out = append(out, tLine{"", ""})
						add("you", "❯ ", it.Text, 0)
					}
				} else {
					out = append(out, tLine{"", ""})
					add("", "● ", it.Text, 0)
				}
			case "tool_use":
				add("tool", "⏺ ", it.Name+"("+toolSummary(it.Input)+")", 1)
			case "tool_result":
				add("dim", "  ⎿ ", resultText(it.Content), 3)
			}
		}
	}
	return out
}

// toolSummary is the gist of a tool call's input: the command, file or pattern.
func toolSummary(raw json.RawMessage) string {
	var in map[string]any
	if json.Unmarshal(raw, &in) != nil {
		return ""
	}
	for _, k := range []string{"command", "file_path", "pattern", "url", "query", "description", "prompt", "path"} {
		if v, ok := in[k].(string); ok && v != "" {
			v = strings.ReplaceAll(v, "\n", " ")
			if r := []rune(v); len(r) > 140 {
				v = string(r[:140]) + "…"
			}
			return v
		}
	}
	return ""
}

func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var items []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &items) == nil {
		var b []string
		for _, it := range items {
			if it.Type == "text" {
				b = append(b, it.Text)
			}
		}
		return strings.Join(b, "\n")
	}
	return ""
}

func transcriptHTML(ls []tLine) string {
	var b strings.Builder
	for _, l := range ls {
		t := html.EscapeString(l.text)
		if l.cls != "" {
			b.WriteString(`<span class="t-` + l.cls + `">` + t + `</span>`)
		} else {
			b.WriteString(t)
		}
		b.WriteByte('\n')
	}
	return b.String()
}
