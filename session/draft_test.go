package session

import (
	"testing"
	"time"
)

func TestWipedDraftsLog(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	SaveWipedDraft("Баги im: 5 задач", "фы\nвторая")
	w := ReadRecentWipes(time.Minute)
	got, ok := w["Баги im: 5 задач"]
	if !ok || got.Text != "фы ⏎ вторая" {
		t.Fatalf("got %+v", w)
	}
}

func TestStaleDraft(t *testing.T) {
	i := &Instance{}
	if i.DraftBlocks() || i.DraftIsStale() {
		t.Fatal("no draft — nothing blocks")
	}
	i.Draft, i.DraftSince = "фы", time.Now()
	if !i.DraftBlocks() {
		t.Fatal("a fresh draft blocks automatic messages")
	}
	i.DraftSince = time.Now().Add(-StaleDraftAfter - time.Second)
	if i.DraftBlocks() || !i.DraftIsStale() {
		t.Fatal("an old draft is forgotten and may be erased")
	}
}
