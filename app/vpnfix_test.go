package app

import (
	"testing"
	"time"
)

func TestVPNFixerReopensThenTells(t *testing.T) {
	var reopened, notes []string
	defer func(f func(string)) { restartVPNApp = f }(restartVPNApp)
	defer func(f func(string, string)) { notifyUser = f }(notifyUser)
	restartVPNApp = func(app string) { reopened = append(reopened, app) }
	notifyUser = func(title, msg string) { notes = append(notes, title) }

	t0 := time.Date(2026, 11, 5, 12, 0, 0, 0, time.Local)
	var f vpnFixer
	f.tick("", t0, t0.Add(time.Hour)) // no app configured: nothing
	if len(reopened) != 0 {
		t.Fatal("reopened without an app")
	}
	f.tick("IdecoClient", t0, t0.Add(6*time.Minute))
	if len(reopened) != 0 {
		t.Fatal("reopened too early")
	}
	f.tick("IdecoClient", t0, t0.Add(7*time.Minute))
	f.tick("IdecoClient", t0, t0.Add(8*time.Minute))
	if len(reopened) != 1 || len(notes) != 0 {
		t.Fatalf("after 8m: reopened %v notes %v", reopened, notes)
	}
	f.tick("IdecoClient", t0, t0.Add(9*time.Minute))
	if len(notes) != 1 {
		t.Fatalf("expected one warning, got %v", notes)
	}
	for m := 10; m <= 120; m++ {
		f.tick("IdecoClient", t0, t0.Add(time.Duration(m)*time.Minute))
	}
	if len(reopened) != vpnFixMax || len(notes) != 1 {
		t.Fatalf("reopened %d times, notes %v", len(reopened), notes)
	}
}
