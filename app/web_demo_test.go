package app

import (
	"claude-squad/config"
	"claude-squad/session"
	"claude-squad/session/tmux"
	"claude-squad/ui"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/stretchr/testify/require"
)

// Screens the fake Claude shows in the web demo.
var demoScreens = map[string]string{
	"perm": `❯ запушь фикс\n\n⏺ Bash(git push --force origin im/escape-fix)\n\n╭──────────────────────────────────────────╮\n│ Bash command                              │\n│                                           │\n│   git push --force origin im/escape-fix   │\n│                                           │\n│ Do you want to proceed?                   │\n│ ❯ 1. Yes                                  │\n│   2. No, and tell Claude what to do differently (esc) │\n╰──────────────────────────────────────────╯\n`,
	"menu": `\n ☐ Release\n\nСоздать релизную ветку сейчас?\n\n❯ 1. Да, создать (Recommended)\n     ветка release/26.1 от master\n  2. Пока не создавать\n  3. Type something.\n────────────────────────\n  4. Chat about this\n\nEnter to select · ↑/↓ to navigate · Esc to cancel\n`,
	"cond": `❯ [squad] Изменения с прошлой сводки: #3 — нужно разрешение\n\n● Разобрал сводку.\n⚠ НУЖЕН ТЫ: #3 ждёт разрешения на git push --force — подтверди\n\n────────────────\n❯ \n────────────────\n`,
	"idle": `❯ проверь задачу 771245\n\n● Проверил: 12 из 12 сценариев ок, отчёт QA-771245-29.10.pdf\n\n────────────────\n❯ \n────────────────\n`,
}

const demoClaude = `#!/bin/bash
D="$(cd "$(dirname "$0")" && pwd)"
printf "$(cat "$D/screen")"
while IFS= read -r line; do printf '\n❯ %s\n\n● ок, делаю\n────────────────\n❯ \n────────────────\n' "$line"; done
`

// TestWebDemo serves the web interface with fake sessions for a manual look:
// SQUAD_WEB_DEMO=1 go test ./app -run TestWebDemo -timeout 30m
func TestWebDemo(t *testing.T) {
	if os.Getenv("SQUAD_WEB_DEMO") == "" {
		t.Skip("manual demo")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	t.Setenv("HOME", t.TempDir())
	state := config.LoadState()
	storage, err := session.NewStorage(state)
	require.NoError(t, err)
	h := &home{ctx: context.Background(), appConfig: config.DefaultConfig(), storage: storage, appState: state, state: stateDefault}
	h.appConfig.ConductorIntervalMinutes = -1
	h.appConfig.WebPort = 8799
	h.appConfig.Pet = "all"
	h.density, h.hasDensity = 3.2, true
	h.tabbedWindow = ui.NewTabbedWindow(ui.NewPreviewPane(), ui.NewDiffPane(), ui.NewTerminalPane())
	h.menu = ui.NewMenu()
	sp := spinner.New()
	h.list = ui.NewList(&sp, false)
	add := func(title, screen string, conductor bool) {
		dir := t.TempDir()
		bin := filepath.Join(dir, "claude")
		require.NoError(t, os.WriteFile(bin, []byte(demoClaude), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "screen"), []byte(demoScreens[screen]), 0644))
		color := ""
		if title == "Релиз 26.1" {
			color = "pink"
		}
		inst, err := session.NewInstanceInFolder(session.SessionRequest{Title: title, Dir: t.TempDir(), Conductor: conductor, Assistant: title == "Помощник", Color: color, Note: "задача 7712 · " + title}, bin)
		require.NoError(t, err)
		h.list.AddInstance(inst)()
		require.NoError(t, inst.Start(true))
		t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", "="+tmux.TmuxNameFor(title)).Run() })
	}
	add("Дирижёр", "cond", true)
	add("Escape в окне вложений", "perm", false)
	add("Релиз 26.1", "menu", false)
	add("QA подсказки по наведению", "idle", false)
	add("Помощник", "idle", false)
	add("Фиксы тестов", "idle", false)
	add("Аудит тестов", "idle", false)
	add("Реакции: пикер и прыжки ленты", "idle", false)
	h.assignNumbers()
	h.startWeb()
	urls, _ := WebURLs()
	t.Logf("web: %v", urls)
	_ = os.WriteFile(filepath.Join(os.Getenv("SQUAD_WEB_DEMO")), []byte(urls[0]), 0644)
	deadline := time.Now().Add(25 * time.Minute)
	for time.Now().Before(deadline) {
		tick(h)
		time.Sleep(time.Second)
	}
}
