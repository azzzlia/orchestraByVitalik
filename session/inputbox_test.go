package session

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInputLineStartingWithNumber(t *testing.T) {
	rule := strings.Repeat("─", 40)
	draft := "⏺ ответ\n" + rule + "\n❯ 1. только давно не обновлявшиеся 2. да можно\n" + rule + "\n  ⏵⏵ auto mode on\n"
	require.True(t, InputBoxVisible(draft), "a message starting with «1.» is the input, not a menu")
	menu := "Do you want to proceed?\n❯ 1. Yes\n  2. No, and tell Claude what to do differently (esc)\n"
	require.False(t, InputBoxVisible(menu))
	last := "Do you want to proceed?\n  1. Yes\n  2. Yes, and don't ask again\n❯ 3. No, and tell Claude what to do differently (esc)\n"
	require.False(t, InputBoxVisible(last), "the cursor on the last option is still a menu")
	long := "Что дальше?\n  1. Да\n     описание\n     ещё\n     ещё\n     ещё\n     ещё\n❯ 2. Нет\n     описание\n"
	require.False(t, InputBoxVisible(long))
}

func TestLongDraftAndDimText(t *testing.T) {
	rule := strings.Repeat("─", 40)
	var b strings.Builder
	b.WriteString("⏺ ответ\n" + rule + "\n\x1b[39m❯ \x1b[2mпервая строка длинного сообщения\x1b[0m\n")
	for i := 0; i < 20; i++ {
		b.WriteString("  продолжение\n")
	}
	b.WriteString(rule + "\n  (•_•) плт 5,0\n  ⏵⏵ auto mode on\n")
	raw := b.String()
	require.True(t, InputBoxVisible(raw), "a long draft starts higher than the last 16 lines")
	require.Equal(t, "", ExtractDraft(raw), "dim text looks like a suggestion to ExtractDraft")
	require.True(t, strings.HasPrefix(InputBoxText(raw), "первая строка длинного сообщения продолжение"))
}
