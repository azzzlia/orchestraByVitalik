package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTranscriptLines(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.jsonl")
	data := `{"type":"user","message":{"role":"user","content":"проверь задачу 42"}}
{"type":"user","isMeta":true,"message":{"content":"<system>skip</system>"}}
{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"hm"},{"type":"text","text":"Смотрю."},{"type":"tool_use","name":"Bash","input":{"command":"git status"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","content":"On branch main\nnothing to commit\nline3\nline4"}]}}
{"type":"summary","summary":"x"}
broken line
{"type":"assistant","message":{"content":[{"type":"text","text":"Готово:\nвсё ок"}]}}
`
	if err := os.WriteFile(p, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, l := range transcriptLines(p) {
		b.WriteString(l.text + "\n")
	}
	got := b.String()
	for _, want := range []string{"❯ проверь задачу 42", "● Смотрю.", "⏺ Bash(git status)", "  ⎿ On branch main", "● Готово:\n  всё ок"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "skip") || strings.Contains(got, "line4") || strings.Contains(got, "hm") {
		t.Errorf("meta, thinking or long result leaked:\n%s", got)
	}
	if h := transcriptHTML([]tLine{{"you", "<b>"}}); !strings.Contains(h, "&lt;b&gt;") {
		t.Errorf("not escaped: %s", h)
	}
}
