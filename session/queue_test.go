package session

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUserQueueOrder(t *testing.T) {
	now := time.Now()
	mk := func(title string) *Instance {
		i := &Instance{Title: title, Status: Ready, started: true, IdleSince: now.Add(-time.Hour)}
		return i
	}
	perm := mk("perm")
	perm.applyPane(" Bash command\n\n   git push --force origin x\n\n Do you want to proceed?\n ❯ 1. Yes\n   2. No, and tell Claude what to do differently (esc)\n\n Esc to cancel · Tab to amend\n", false, false)
	dec := mk("dec")
	dec.MarkedFor, dec.MarkReason = "user", "выбрать стенд"
	calls := &Instance{Title: "cond", Conductor: true, Status: Ready, started: true}
	calls.applyPane("● Итог.\n⚠ НУЖЕН ТЫ: #2 merge\n────\n❯ \n────\n", false, false)
	busy := mk("busy")
	busy.Status = Running
	asst := &Instance{Title: "a", Assistant: true, Status: Ready, started: true}
	q := UserQueue([]*Instance{busy, dec, perm, asst, calls})
	require.Len(t, q, 3)
	require.Equal(t, QueueCalls, q[0].Kind)
	require.Equal(t, QueuePermission, q[1].Kind)
	require.Equal(t, "perm", q[1].Inst.Title)
	require.Equal(t, QueueDecision, q[2].Kind)
	require.Equal(t, "выбрать стенд", q[2].Text)
}

func TestCapacityIdleFreesPlace(t *testing.T) {
	snap := &StatusSnapshot{Sessions: []StatusEntry{
		{Number: 1, Title: "a", Status: "в работе"},
		{Number: 2, Title: "b", Status: "ждёт дирижёра", IdleMinutes: 45},
		{Number: 3, Title: "c", Status: "в ожидании · фоновая задача", IdleMinutes: 90},
		{Number: 4, Title: "d", Status: "ждёт дирижёра", IdleMinutes: 5},
	}}
	c := ComputeCapacity(snap, 6)
	require.Equal(t, 3, c.Busy)
	require.Len(t, c.IdleList, 1)
}

func TestGroupOf(t *testing.T) {
	require.Equal(t, GroupQA, (&Instance{Title: "QA sidebar-entities"}).GroupOf())
	require.Equal(t, GroupQA, (&Instance{Title: "x", Note: "проверка задачи 771245"}).GroupOf())
	require.Equal(t, GroupDev, (&Instance{Title: "Фиксы тестов"}).GroupOf())
	require.Equal(t, GroupDev, (&Instance{Title: "QA voice", Group: GroupDev}).GroupOf())
	require.Equal(t, "main", (&Instance{Title: "Дирижёр", Conductor: true}).GroupOf())
	require.Equal(t, "Релизы", (&Instance{Title: "QA x", Group: "Релизы"}).GroupOf())
	g, ok := NormGroup("проверка")
	require.True(t, ok)
	require.Equal(t, GroupQA, g)
}
