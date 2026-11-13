package app

import (
	"claude-squad/config"
	"claude-squad/session"
	"claude-squad/ui"
	"claude-squad/ui/overlay"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Bug 27.10: a window left open in Squad held up cs ctl new/kill for hours.
func TestUnblockWindows(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	defer func(f func(string, string)) { notifyUser = f }(notifyUser)
	notes := 0
	notifyUser = func(string, string) { notes++ }

	h := &home{ctx: context.Background(), state: stateConfirm, appConfig: config.DefaultConfig(), menu: ui.NewMenu(),
		confirmationOverlay: overlay.NewConfirmationOverlay("Закрыть сессию?")}
	now := time.Now()
	h.lastKeyAt = now.Add(-5 * time.Minute)

	h.unblockWindows(now) // nothing waits: the window stays
	if h.state != stateConfirm {
		t.Fatal("окно закрыто, хотя ничего не ждёт")
	}
	_, err := session.SubmitSessionRequest(session.SessionRequest{Title: "Новая", Dir: "/tmp", At: now.Add(-3 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	h.lastKeyAt = now.Add(-30 * time.Second) // the user is answering right now
	h.unblockWindows(now)
	if h.state != stateConfirm {
		t.Fatal("закрыли окно, в котором пользователь только что нажимал клавиши")
	}
	h.lastKeyAt = now.Add(-5 * time.Minute)
	h.unblockWindows(now)
	if h.state != stateDefault || h.confirmationOverlay != nil {
		t.Fatal("забытое подтверждение не закрыто")
	}

	// a window with typing is never closed — the user is told once
	h.state = statePrompt
	h.unblockWindows(now)
	h.unblockWindows(now)
	if h.state != statePrompt || notes != 1 {
		t.Fatalf("поле ввода: state=%v, уведомлений %d", h.state, notes)
	}
	dir, _ := config.GetConfigDir()
	if _, err := os.Stat(filepath.Join(dir, "requests")); err != nil {
		t.Fatal("запрос пропал из очереди")
	}
}
