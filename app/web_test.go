package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
)

func TestAnsiToHTML(t *testing.T) {
	out := ansiToHTML([]string{"\x1b[1;31mОшибка\x1b[0m <b>", "\x1b]8;;http://x\x07ссылка\x1b]8;;\x07 ok"}, 2, 1)
	require.Contains(t, out, `<span style="color:#f7768e;font-weight:700;">Ошибка</span>`)
	require.Contains(t, out, "&lt;b&gt;")
	require.NotContains(t, out, "http://x")
	require.Contains(t, out, `class="cur"`)
	require.Equal(t, 1, strings.Count(out, `class="cur"`))
	// the cursor past the end of the line is drawn after padding
	require.Contains(t, ansiToHTML([]string{"ab"}, 5, 0), "   <span class=\"cur\"")
	require.Contains(t, ansiToHTML([]string{"\x1b[38;2;255;0;0mR"}, -1, 0), "color:#ff0000")
}

func TestLatinKey(t *testing.T) {
	m := latinKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'м'}})
	require.Equal(t, "v", m.String())
	m = latinKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Т'}})
	require.Equal(t, "N", m.String())
	m = latinKey(tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, "enter", m.String())
}
