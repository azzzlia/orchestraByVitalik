package session

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProgressSignatureIgnoresSpinner(t *testing.T) {
	a := "❯ x\n⏺ gitlab - get (MCP)(id: 5)\n✻ Pondering… (12s · esc to interrupt)\n"
	b := "❯ x\n⏺ gitlab - get (MCP)(id: 5)\n✶ Pondering… (1m 33s · ↓ 2k tokens · esc to interrupt)\n"
	c := "❯ x\n⏺ gitlab - get (MCP)(id: 5)\n  ⎿ {\"ok\":true}\n"
	require.Equal(t, progressSignature(a), progressSignature(b))
	require.NotEqual(t, progressSignature(a), progressSignature(c))
}

func TestClassifyNetError(t *testing.T) {
	n, m := classifyNetError("❯ x\n⏺ mantis - get_issue (MCP)\n  ⎿ Error: MCP error -32000: Connection closed\n")
	require.True(t, n)
	require.True(t, m)
	n, m = classifyNetError("❯ x\n  ⎿ API Error: Connection error.\n")
	require.True(t, n)
	require.False(t, m)
	n, _ = classifyNetError("❯ x\n● всё прошло\n")
	require.False(t, n)
}

func TestLastToolIsMCP(t *testing.T) {
	require.True(t, lastToolIsMCP("❯ x\n⏺ Bash(ls)\n  ⎿ a\n⏺ gitlab - get_merge_request (MCP)(id: 5)\n✻ Working…"))
	require.False(t, lastToolIsMCP("❯ x\n⏺ gitlab - get (MCP)(id: 5)\n  ⎿ ok\n⏺ Bash(npm test)\n✻ Working…"))
}

func TestWaitReason(t *testing.T) {
	cases := map[string]string{
		"❯ x\n⏺ Bash(sleep 300 && glab ci status)\n✻ Working…":        "пайплайн",
		"❯ x\n⏺ Bash(sleep 600)\n✻ Working…":                          "таймер",
		"❯ x\n⏺ Bash(until curl -s x; do sleep 10; done)\n✻ Working…": "опрос",
		"❯ x\n⏺ Bash(npm test)\n✻ Working…":                           "",
		"❯ x\n⏺ gitlab - get_merge_request (MCP)(id: 5)\n✻ Working…":  "",
		"❯ x\n⏺ Bash(sleep 5)\n  ⎿ ok\n⏺ Готово, жду ревью.\n":        "таймер",
		"❯ x\n⏺ Read(a.go)\n  ⎿ ok\n⏺ Готово.\n":                      "",
	}
	for turn, want := range cases {
		require.Equal(t, want, waitReason(turn, turn), turn)
	}
	require.Equal(t, "фоновая задача", waitReason("❯ x\n● Готово\n", "❯ x\n● Готово\n────\n❯ \n────\n  ⏵⏵ accept edits · 1 background task"))
}

func TestSuggestionWithCursorOnFirstLetterIsNotADraft(t *testing.T) {
	screen := "────\n\x1b[2m❯ \x1b[22m\x1b[7mT\x1b[27m\x1b[2mry \"fix lint errors\"\x1b[22m\n────\n"
	require.Equal(t, "", extractDraft(screen))
	require.Equal(t, "Try \"fix lint errors\"", extractSuggestion(screen))
	// Real typed text with the cursor after it is still a draft.
	typed := "────\n❯ привет\x1b[7m \x1b[27m\n────\n"
	require.Equal(t, "привет", extractDraft(typed))
	// Typed text with the cursor moved onto its first letter is a draft too.
	typed2 := "────\n❯ \x1b[7mп\x1b[27mривет\n────\n"
	require.Equal(t, "привет", extractDraft(typed2))
}

func TestParseDensity(t *testing.T) {
	for in, want := range map[string]float64{"10,4": 10.4, "10.4\n": 10.4, "плотность 7,25 ⚡": 7.25, "6": 6} {
		v, ok := ParseDensity(in)
		require.True(t, ok, in)
		require.InDelta(t, want, v, 1e-9, in)
	}
	_, ok := ParseDensity("нет")
	require.False(t, ok)
}

func TestOldBoxStyleInputIsRecognised(t *testing.T) {
	screen := "╭────────────────╮\n│ > привет       │\n╰────────────────╯\n  ? for shortcuts\n"
	require.True(t, InputBoxVisible(screen))
	require.Equal(t, "привет", ExtractDraft(screen))
	empty := "╭────────────────╮\n│ >              │\n╰────────────────╯\n"
	require.True(t, InputBoxVisible(empty))
	require.Equal(t, "", ExtractDraft(empty))
}

func TestDetectLimit(t *testing.T) {
	now := time.Date(2026, 10, 28, 1, 10, 0, 0, time.Local)
	hit, at := detectLimit("❯ x\n  ⎿ API Error: 403 Request not allowed · Please run /login\n", now)
	require.True(t, hit)
	require.True(t, at.IsZero())
	hit, at = detectLimit("❯ x\n5-hour limit reached ∙ resets 3am\n", now)
	require.True(t, hit)
	require.Equal(t, 3, at.Hour())
	hit, at = detectLimit("❯ x\nYou've hit your limit · resets 11:30pm\n", now)
	require.True(t, hit)
	require.Equal(t, 23, at.Hour())
	require.Equal(t, 30, at.Minute())
	hit, _ = detectLimit("❯ x\n● Всё готово\n", now)
	require.False(t, hit)
	// The agent talking about rate limits, or the user's task mentioning one, is not a limit.
	hit, _ = detectLimit("❯ добавь ретраи на rate limit\n  и usage limit reached в логах\n\n● Добавил ретраи, когда API отвечает rate limit (429).\n", now)
	require.False(t, hit)
}

func TestParseT39(t *testing.T) {
	v, ok := ParseT39("\x1b[2mплт\x1b[0m \x1b[32m9.2\x1b[0m ▱▱▱▱▱▱ 0ч ⧗6м→10:00")
	require.True(t, ok)
	require.InDelta(t, 9.2, v, 1e-9)
	v, ok = ParseT39("плотность: 10,4 · 79 место")
	require.True(t, ok)
	require.InDelta(t, 10.4, v, 1e-9)
	_, ok = ParseT39("нет данных")
	require.False(t, ok)
}
