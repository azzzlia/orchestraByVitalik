package session

import (
	"testing"
	"time"
)

func TestWhoseTurn(t *testing.T) {
	defer func() { ConductorActive = false }()
	w := &Instance{Status: Ready, IdleSince: time.Now()}
	label := func(i *Instance) string { l, _ := i.StatusLabel(); return l }

	ConductorActive = false
	if got := label(w); got != "ждёт тебя" {
		t.Fatalf("no conductor: %q", got)
	}
	ConductorActive = true
	if got, kind := w.StatusLabel(); got != "ждёт дирижёра" || kind != StatusConductor {
		t.Fatalf("conductor on: %q", got)
	}
	w.IdleSince = time.Now().Add(-ConductorGiveUp - time.Minute)
	if got := label(w); got != "ждёт тебя · дирижёр не разобрал" {
		t.Fatalf("not handled: %q", got)
	}
	w.IdleSince = time.Now()
	w.MarkedFor, w.MarkReason = "user", "проверить на портале"
	if got := label(w); got != "ждёт тебя · проверить на портале" {
		t.Fatalf("marked user: %q", got)
	}
	w.MarkedFor, w.MarkReason = "wait", "ревью MR"
	if got, kind := w.StatusLabel(); got != "в ожидании · ревью MR" || kind != StatusOnHold {
		t.Fatalf("marked wait: %q", got)
	}
	w.Status = Running
	w.ClearMarkWhenWorking()
	if w.MarkedFor != "" {
		t.Fatal("mark is cleared when the agent works again")
	}
	w.Status = Ready
	w.Draft, w.DraftSince = "фы", time.Now()
	if got := label(w); got != "ты печатаешь" {
		t.Fatalf("draft: %q", got)
	}
	c := &Instance{Status: Ready, Conductor: true}
	if got, kind := c.StatusLabel(); got != "на связи" || kind != StatusStandby {
		t.Fatalf("conductor: %q", got)
	}
	a := &Instance{Status: Ready, Assistant: true}
	if got := label(a); got != "ответил" {
		t.Fatalf("assistant: %q", got)
	}
}
