package ui

import (
	"claude-squad/session"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func layoutFixture() []*session.Instance {
	var list []*session.Instance
	add := func(n int, title string, f func(*session.Instance)) {
		i := &session.Instance{Title: title, Number: n, Status: session.Ready,
			TailLines: []string{"строка экрана 1", "строка экрана 2", "строка экрана 3"}}
		if f != nil {
			f(i)
		}
		list = append(list, i)
	}
	add(11, "Дирижёр", func(i *session.Instance) { i.Conductor = true })
	add(15, "Помощник", func(i *session.Instance) { i.Assistant = true })
	for _, n := range []int{2, 4, 6, 16, 17, 18, 19, 21, 22, 23} {
		add(n, fmt.Sprintf("воркер %d", n), nil)
	}
	for _, n := range []int{7, 10, 13} {
		add(n, fmt.Sprintf("спящий %d", n), func(i *session.Instance) { i.Status = session.Paused })
	}
	return list
}

func TestOverviewLayout(t *testing.T) {
	list := layoutFixture()
	l := ComputeOverviewLayout(list, 250)
	require.Equal(t, []int{0, 1}, l.Main)
	require.Len(t, l.Grid, 10)
	require.Equal(t, []int{12, 13, 14}, l.Sleep)
	require.Equal(t, 3, l.Cols)

	out := session.StripANSI(RenderOverview(list, 0, 250, 60))
	lines := strings.Split(out, "\n")
	require.LessOrEqual(t, len(lines), 60)
	require.Contains(t, lines[len(lines)-1], "спят: 7 спящий 7 · 10 спящий 10 · 13 спящий 13")
	top := ""
	for _, ln := range lines {
		if strings.Contains(ln, "11. Дирижёр") {
			top = ln
		}
	}
	require.Contains(t, top, "15. Помощник")
	require.NotContains(t, out, "ещё сессии ниже")

	// Navigation: down from the conductor to the first grid row, down to the bottom
	// grid row and into the sleeping line; up from the grid back to the top row.
	require.Equal(t, 2, l.Move(0, "down"))
	require.Equal(t, 4, l.Move(1, "down"))
	require.Equal(t, 1, l.Move(4, "up"))
	require.Equal(t, 0, l.Move(2, "up"))
	require.Equal(t, 5, l.Move(2, "down"))
	require.Equal(t, 12, l.Move(11, "down"))
	require.Equal(t, 11, l.Move(12, "up"))
	require.Equal(t, 1, l.Move(0, "right"))
	require.Equal(t, 2, l.Move(1, "right"))
	require.Equal(t, 13, l.Move(12, "right"))
	require.Equal(t, 0, l.Move(0, "up"))
}

func TestOverviewLayoutSmallScreen(t *testing.T) {
	// 200x50: everything still fits without scrolling.
	out := session.StripANSI(RenderOverview(layoutFixture(), 13, 200, 50))
	require.NotContains(t, out, "ещё сессии ниже")
	require.LessOrEqual(t, len(strings.Split(out, "\n")), 50)
}

func TestOverviewFitsWithLongStatusAndTinyScreens(t *testing.T) {
	list := layoutFixture()
	list[2].NeedsInput = true
	list[2].PermissionCmd = strings.Repeat("git push -u origin very/long/branch/name ", 5)
	for _, h := range []int{32, 44, 52, 60} {
		out := RenderOverview(list, 0, 250, h)
		require.LessOrEqual(t, len(strings.Split(out, "\n")), h, "height %d", h)
	}
	for h := 0; h < 8; h++ {
		require.NotPanics(t, func() { RenderOverview(list, 0, 120, h) })
	}
}
