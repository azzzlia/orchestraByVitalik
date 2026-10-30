package ui

import (
	"fmt"
	"strings"
	"testing"

	"claude-squad/session"
)

func TestOverviewFitsSmallHeights(t *testing.T) {
	var list []*session.Instance
	list = append(list, &session.Instance{Title: "дирижёр", Conductor: true, Status: session.Ready})
	list = append(list, &session.Instance{Title: "помощник", Assistant: true, Status: session.Ready})
	for i := 0; i < 7; i++ {
		list = append(list, &session.Instance{Title: fmt.Sprintf("w%d", i), Status: session.Running})
	}
	for i := 0; i < 4; i++ {
		list = append(list, &session.Instance{Title: fmt.Sprintf("s%d", i), Status: session.Paused})
	}
	for h := 6; h <= 30; h++ {
		for _, w := range []int{60, 120, 200} {
			out := RenderOverview(list, 0, w, h)
			if n := strings.Count(out, "\n") + 1; n > h {
				t.Errorf("w=%d h=%d: %d lines", w, h, n)
			}
		}
	}
}
