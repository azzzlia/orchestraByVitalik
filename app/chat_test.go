package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChatHTML(t *testing.T) {
	bs := []tBlock{
		{"you", "Составь план <проверки>", "09:41"},
		{"tool", "Read(BRIEF.md)", "09:42"},
		{"res", "…", ""},
		{"tool", "Read(sound.js)", "09:42"},
		{"tool", "Bash(git branch --show-current)", "09:42"},
		{"ai", "## План\n| ID | Ожидание |\n|---|---|\n| S-04b | один звук |\n\n**Нужно твоё решение:** эмуляция или десктоп?", "09:43"},
		{"you", "[squad] Сеть вернулась", "09:50"},
	}
	h := chatHTML(bs)
	require.Equal(t, 3, strings.Count(h, `class="c-row"`), h)
	require.Contains(t, h, "Составь план &lt;проверки&gt;")
	require.Contains(t, h, `<span class="c-chip">Read ×2</span>`)
	require.Contains(t, h, `Bash git branch --show-current`)
	require.Contains(t, h, `<th>ID</th><th>Ожидание</th>`)
	require.Contains(t, h, `<td>S-04b</td><td>один звук</td>`)
	require.Contains(t, h, `class="t-need"`)
	require.Contains(t, h, `c-who c-sq">Squad`)
	require.Contains(t, mdHTML("| a | b |\n|---|---|\n| x<br>y | z |"), "<td>x<br>y</td>")
	require.Less(t, strings.Index(h, "c-chips"), strings.Index(h, "t-tbl"), "chips before the answer")
}

// The cache reads only what was appended, and waits for a line still being written.
func TestTranscriptBlocksCached(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.jsonl")
	line := func(text string) string {
		return `{"type":"user","timestamp":"2026-11-06T09:00:00Z","message":{"content":"` + text + `"}}` + "\n"
	}
	require.NoError(t, os.WriteFile(p, []byte(line("один")+line("два")), 0600))
	require.Len(t, transcriptBlocksCached(p), 2)
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0600)
	_, _ = f.WriteString(line("три") + `{"type":"user","mess`)
	require.Len(t, transcriptBlocksCached(p), 3, "a half-written line is not read yet")
	_, _ = f.WriteString(`age":{"content":"четыре"}}` + "\n")
	f.Close()
	bs := transcriptBlocksCached(p)
	require.Len(t, bs, 4)
	require.Equal(t, "четыре", bs[3].text)
}

func TestChatHTMLFoldsLongToolRuns(t *testing.T) {
	var bs []tBlock
	for k := 0; k < 10; k++ {
		bs = append(bs, tBlock{"tool", "Bash(step " + string(rune('a'+k)) + ")", "10:00"})
	}
	h := chatHTML(bs)
	require.Contains(t, h, `<summary>ещё 6</summary>`)
	require.Equal(t, 10, strings.Count(h, `class="c-chip"`))
}
