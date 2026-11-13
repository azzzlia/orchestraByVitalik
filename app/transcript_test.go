package app

import (
	"claude-squad/session"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
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
	bs := transcriptBlocks(p)
	h := transcriptHTML(bs)
	for _, want := range []string{`<div class="t-you">`, "проверь задачу 42", `<div class="t-ai"><div>Смотрю.</div></div>`, "⏺ Bash(git status)", "⎿ On branch main", "<div>Готово:</div><div>всё ок</div>"} {
		if !strings.Contains(h, want) {
			t.Errorf("no %q in:\n%s", want, h)
		}
	}
	if strings.Contains(h, "skip") || strings.Contains(h, "line4") || strings.Contains(h, "hm") {
		t.Errorf("meta, thinking or long result leaked:\n%s", h)
	}
	if h := transcriptHTML([]tBlock{{"you", "<b>", ""}}); !strings.Contains(h, "&lt;b&gt;") {
		t.Errorf("not escaped: %s", h)
	}
	// a page never starts in the middle of an answer
	long := []tBlock{{"you", "q", ""}, {"ai", strings.Repeat("строка\n", 50) + "конец", ""}, {"you", "ещё", ""}}
	page, n, more := lastBlocks(long, 10)
	require.Len(t, page, 2)
	require.Equal(t, 52, n)
	require.True(t, more)
}

func TestMarkdownHTML(t *testing.T) {
	h := mdHTML("## Итог\n**Голосовые:** нужен `aqua-setup`\n- раз\n  - два\n```\n<x> & y\n```")
	for _, want := range []string{`<div class="t-h">Итог</div>`, "<b>Голосовые:</b>", "<code>aqua-setup</code>", `<span class="t-bu">•</span><span class="t-lt">раз</span>`, `<div class="t-code">&lt;x&gt; &amp; y`} {
		require.Contains(t, h, want)
	}
}

func TestReadFirstAsk(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.jsonl")
	body := `{"type":"user","isMeta":true,"message":{"content":"<command-name>/clear</command-name>"}}
{"type":"user","message":{"content":"<local-command-stdout></local-command-stdout>"}}
{"type":"user","message":{"content":[{"type":"text","text":"проверь задачу 771245:\n  поиск по эмодзи ` + session.WorkerNoMenuNote + `"}]}}
{"type":"user","message":{"content":"второе сообщение"}}
`
	require.NoError(t, os.WriteFile(p, []byte(body), 0644))
	require.Equal(t, "проверь задачу 771245: поиск по эмодзи", readFirstAsk(p))
}
