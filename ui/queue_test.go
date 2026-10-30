package ui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"claude-squad/session"
)

func TestRenderQueueFits(t *testing.T) {
	var items []session.QueueItem
	for i := 0; i < 12; i++ {
		inst := &session.Instance{Title: "сессия с длинным названием", Number: i + 1}
		inst.Menu = &session.ChoiceMenu{Question: "Что дальше?", Options: []session.MenuOption{{Number: 1, Label: "Да"}, {Number: 2, Label: "Нет"}}}
		items = append(items, session.QueueItem{Index: i, Inst: inst, Kind: session.QueueMenu, Text: strings.Repeat("очень длинный текст ", 30)})
	}
	for _, sel := range []int{0, 5, 11} {
		for _, h := range []int{10, 20, 40} {
			out := RenderQueue(items, sel, 80, h, time.Now())
			if n := strings.Count(out, "\n") + 1; n > h {
				t.Errorf("sel=%d h=%d: %d lines", sel, h, n)
			}
			if !strings.Contains(session.StripANSI(out), "#"+strconv.Itoa(sel+1)+" ") {
				t.Errorf("sel=%d h=%d: selected row not shown", sel, h)
			}
			for _, l := range strings.Split(out, "\n") {
				if w := len([]rune(session.StripANSI(l))); w > 80 {
					t.Errorf("line too wide (%d): %q", w, session.StripANSI(l))
				}
			}
		}
	}
	if !strings.Contains(RenderQueue(nil, 0, 80, 20, time.Now()), "Никто не ждёт") {
		t.Error("empty queue")
	}
}
