package app

import (
	"encoding/json"
	"strings"

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
	if md := os.Getenv("SQUAD_UPD_DEMO_MD"); md != "" { // панель «Обновления»
		dir, _ := config.GetConfigDir()
		ud := filepath.Join(dir, "updates")
		require.NoError(t, os.MkdirAll(ud, 0o755))
		b, err := os.ReadFile(md)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(ud, "im-26.1700.0.md"), b, 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(ud, "im-26.1600.0.md"), b, 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(ud, "state.json"), []byte(`{"entries":{
 "im/26.1700.0":{"module":"im","version":"26.1700.0","head":"00c9fde8","in_test":true,"test_note":"Забрали в тест im 26.1700.0","test_at":"06.11 15:10","done_head":"00c9fde8","done_at":"09.11 18:20"},
 "im/26.1800.0":{"module":"im","version":"26.1800.0","head":"bf2ed54c","status":"running"},
 "im/26.1600.0":{"module":"im","version":"26.1600.0","head":"2b5022b6","done_head":"1111","done_at":"23.10 11:02","new":6},
 "im/26.1500.0":{"module":"im","version":"26.1500.0","head":"eb346a55","status":"error","err":"нет предыдущей ветки обновления im — не с чем сравнить"}},
 "chat_err":"","scan_at":"09.11 18:12"}`), 0o644))
	}
	state := config.LoadState()
	storage, err := session.NewStorage(state)
	require.NoError(t, err)
	h := &home{ctx: context.Background(), appConfig: config.DefaultConfig(), storage: storage, appState: state, state: stateDefault}
	h.appConfig.ConductorIntervalMinutes = -1
	h.appConfig.WebPort = 8799
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
	demoTranscript(t, "QA подсказки по наведению")
	demoTranscript(t, "Escape в окне вложений")
	demoTranscript(t, "Релиз 26.1")
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

// demoTranscript gives a demo session a Claude Code transcript, for the chat view.
func demoTranscript(t *testing.T, title string) {
	home, _ := os.UserHomeDir()
	conv := "0de0c0de-7820-5900-0000-000000000001"
	dir := filepath.Join(home, ".claude-squad", "claude-sessions")
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, tmux.TmuxNameFor(title)), []byte(conv+"\n"), 0600))
	pdir := filepath.Join(home, ".claude", "projects", "demo")
	require.NoError(t, os.MkdirAll(pdir, 0700))
	var b strings.Builder
	at := time.Date(2026, 11, 6, 9, 38, 0, 0, time.Local)
	put := func(typ string, content any) {
		at = at.Add(70 * time.Second)
		line, _ := json.Marshal(map[string]any{"type": typ, "timestamp": at.UTC().Format(time.RFC3339Nano), "message": map[string]any{"content": content}})
		b.Write(line)
		b.WriteByte('\n')
	}
	tool := func(name, key, val string) []map[string]any {
		return []map[string]any{{"type": "tool_use", "name": name, "input": map[string]any{key: val}}}
	}
	res := func(text string) []map[string]any {
		return []map[string]any{{"type": "tool_result", "content": text}}
	}
	put("user", "Прочитай задачу 782059 и MR Жени, подготовь бриф")
	put("assistant", tool("mcp__tracker__task_get", "query", "782059"))
	put("user", res("Восстановить входящий звук в десктопе…"))
	put("assistant", tool("Write", "file_path", "BRIEF.md"))
	put("user", res("ok"))
	put("assistant", []map[string]any{{"type": "text", "text": "Бриф готов: 4 критерия приёмки, в объём не входят Cmd+Q, роли окон и мобильный клиент. MR пока в **Draft**, сверю готовность перед арендой."}})
	put("user", "Составь план проверки по брифу и покажи до аренды стенда")
	put("assistant", tool("Read", "file_path", "BRIEF.md"))
	put("user", res("…"))
	put("assistant", tool("Read", "file_path", "im/lib/desktop/sound.js"))
	put("user", res("…"))
	for _, c := range []string{"docker exec -i -u bx bx-php-fpm php -l im/lib/sound.php", "chef test e2e im.v2.sound --project chromium", "git log -1 --format=%B 798240f19bba", "git add im/install/js/im/v2/sound", "sed -i '' 's/16/15/' notes.md", "git commit -q -F msg.txt", "bx codecept run -c im/tests"} {
		put("assistant", tool("Bash", "command", c))
		put("user", res("ok"))
	}
	put("assistant", tool("Bash", "command", "cd ~/.claude/worktrees/im-782059 && git branch --show-current"))
	put("user", res("im/782059-desktop-incoming-sound"))
	rows := []string{"| ID | Сценарий | Где | Кто | Ожидание |", "|---|---|---|---|---|"}
	for k, r := range []string{"упоминание A (AC2)|один звук", "событие-уведомление для A (AC2)|один звук", "статус «Не беспокоить» (AC4)|тишина", "чат с B заглушён (AC4)|тишина", "громкость сообщений 0 (AC4)|тишина", "звук отправки своего сообщения (AC4)|звучит как раньше", "preview звука в настройках (AC4)|звучит"} {
		p := strings.Split(r, "|")
		rows = append(rows, "| S-0"+string(rune('4'+k/3))+string(rune('a'+k%3))+" | "+p[0]+" | macOS | вы, я | "+p[1]+" |")
	}
	answer := "## План проверки 782059\n\nСначала я один прохожу C-01 и C-02. Потом вместе проходим C-03 и C-04: я отправляю события от имени B, вы отвечаете «один звук / тишина / два и больше».\n\n" +
		strings.Join(rows, "\n") + "\n\n**Ограничения:**\n- Десктоп проверяем только на macOS.\n- Сборку и lint не перезапускаю, беру результаты разработчика из `1d2358678cdd`: MR пока в Draft, поэтому перед арендой ещё раз сверю, что ветка не уехала и бандл собран из того же коммита, иначе результаты будут непоказательными.\n\n**Способ проверки:** аренда в Aqua\n\n```\ndeploy-aqua --branch im/782059 --bundle 1d2358678cdd\n```\n\n⚠ Нужно твоё решение: эмуляция в браузере или настоящий десктоп?"
	put("assistant", []map[string]any{{"type": "text", "text": answer}})
	put("user", "[squad] Сеть/VPN пропадала и вернулась. Продолжай задачу с того места, где остановился.")
	require.NoError(t, os.WriteFile(filepath.Join(pdir, conv+".jsonl"), []byte(b.String()), 0600))
}
