package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseChoiceMenuSingle(t *testing.T) {
	raw, err := os.ReadFile("testdata/menu_single.ansi")
	require.NoError(t, err)
	m, ok := ParseChoiceMenu(string(raw))
	require.True(t, ok)
	require.Equal(t, "Which fruit do you want to pick?", m.Question)
	require.Len(t, m.Options, 5)
	require.Equal(t, 1, m.Cursor)
	r, ok := m.Recommended()
	require.True(t, ok)
	require.Equal(t, 1, r.Number)
	require.True(t, m.Options[3].Free)
	require.True(t, m.Options[4].Chat)
	require.False(t, m.Multi)
	require.False(t, m.Risky())
	require.Equal(t, "Which fruit do you want to pick? — 1. Apple (Recommended) · 2. Banana · 3. Cherry · 4. Type something.", m.Summary())
	require.False(t, InputBoxVisible(string(raw)))
}

func TestParseChoiceMenuMulti(t *testing.T) {
	raw, err := os.ReadFile("testdata/menu_multi.ansi")
	require.NoError(t, err)
	m, ok := ParseChoiceMenu(string(raw))
	require.True(t, ok)
	require.True(t, m.Multi)
	require.Equal(t, "Which sizes do you want?", m.Question)
	require.Equal(t, []string{"Color", "Size"}, m.Tabs)
	require.Equal(t, "S", m.Options[0].Label)
	require.True(t, m.Options[3].Free)
}

func TestParseChoiceMenuReviewAndRisky(t *testing.T) {
	review := `←  ☒ Color  ☒ Size  ✔ Submit  →

Review your answers

 ● Which color do you want?
   → red (Recommended)

Ready to submit your answers?

❯ 1. Submit answers
  2. Cancel
`
	m, ok := ParseChoiceMenu(review)
	require.True(t, ok)
	require.True(t, m.Review)
	require.Equal(t, "Submit answers", m.Options[0].Label)

	push := `
 ☐ Push
Что пушим?
❯ 1. Без QA-коммитов (Recommended)
  2. Пушить всё
  3. Пока не пушить
  4. Type something.
────────────
  5. Chat about this
Enter to select · ↑/↓ to navigate · Esc to cancel
`
	m, ok = ParseChoiceMenu(push)
	require.True(t, ok)
	require.True(t, m.Risky())
	require.Equal(t, "Что пушим?", m.Question)
}

func TestNotAMenu(t *testing.T) {
	_, ok := ParseChoiceMenu("План:\n1. сделать\n2. проверить\n❯ \n")
	require.False(t, ok)
	_, ok = ParseChoiceMenu("Do you want to proceed?\n❯ 1. Yes\n  2. No\nEnter to select · Esc to cancel\n")
	require.False(t, ok)
	require.False(t, IsRisky("Which fruit?"))
	require.True(t, IsRisky("Создать MR сейчас?"))
	require.False(t, IsRisky("summary mode"))
}

func TestMenuStatusLabel(t *testing.T) {
	old := ConductorActive
	defer func() { ConductorActive = old }()
	ConductorActive = true
	i := &Instance{Status: Ready, Menu: &ChoiceMenu{Question: "q"}}
	label, kind := i.StatusLabel()
	require.Equal(t, "меню · ждёт дирижёра", label)
	require.Equal(t, StatusConductor, kind)
	i.MenuEscalated = true
	_, kind = i.StatusLabel()
	require.Equal(t, StatusWaiting, kind)
	ConductorActive = false
	i.MenuEscalated = false
	label, _ = i.StatusLabel()
	require.Equal(t, "ждёт тебя · выбор в меню", label)
}

func TestWorkerGetsNoMenuTool(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	w, err := NewInstanceInFolder(SessionRequest{Title: "w", Dir: dir, Prompt: "проверь задачу"}, "claude --model sonnet")
	require.NoError(t, err)
	require.Contains(t, w.Program, "claude --model sonnet --settings '")
	require.True(t, strings.HasSuffix(w.Program, "worker-settings.json' --disallowedTools AskUserQuestion"), w.Program)
	require.Contains(t, w.PendingPrompt, "AskUserQuestion")
	c, err := NewInstanceInFolder(SessionRequest{Title: "c", Dir: dir, Conductor: true}, "claude")
	require.NoError(t, err)
	require.Equal(t, "claude", c.Program)
	x, err := NewInstanceInFolder(SessionRequest{Title: "x", Dir: dir}, "~/bin/claude-qa")
	require.NoError(t, err)
	require.Equal(t, "~/bin/claude-qa", x.Program)
}

func TestParseChoiceMenuReal(t *testing.T) {
	raw, err := os.ReadFile("testdata/menu_real.txt")
	require.NoError(t, err)
	m, ok := ParseChoiceMenu(string(raw))
	require.True(t, ok)
	t.Log(m.Question)
	t.Log(m.Summary())
	require.Len(t, m.Options, 5)
	r, _ := m.Recommended()
	require.Equal(t, "Ветка-спутник (Recommended)", r.Label)
	require.Equal(t, []string{"Ветка клона"}, m.Tabs)
	require.NotContains(t, m.Question, "│")
	require.Contains(t, m.Question, "Ветку im/chatbot-voice-messages держит")
	require.True(t, strings.HasSuffix(m.Question, "im-chatbot-voice-messages?"))
}

func TestTrustPromptStatus(t *testing.T) {
	i := &Instance{Status: Ready}
	i.applyPane("Quick safety check: Is this a project you created or one you trust?\n❯ 1. Yes, I trust this folder\n  2. No, exit\nEnter to confirm · Esc to cancel\n", false, false)
	require.True(t, i.TrustPrompt)
	require.Nil(t, i.Menu)
	label, kind := i.StatusLabel()
	require.Equal(t, "ждёт тебя · подтверди доверие к папке", label)
	require.Equal(t, StatusNeedsInput, kind)
}

func TestPermissionRequest(t *testing.T) {
	raw, err := os.ReadFile("testdata/permission.ansi")
	require.NoError(t, err)
	require.Equal(t, "git push -u origin im/escape-fix", PermissionRequest(string(raw)))
	_, ok := ParseChoiceMenu(string(raw))
	require.False(t, ok, "a permission prompt is not a menu to answer")
	require.Equal(t, "правка app/app.go", PermissionRequest(" Edit file\n Do you want to make this edit to app/app.go?\n ❯ 1. Yes\n"))
	require.Equal(t, "", PermissionRequest("❯ \n"))
	i := &Instance{Status: Ready}
	i.applyPane(string(raw), false, false)
	label, kind := i.StatusLabel()
	require.Equal(t, "нужно разрешение · git push -u origin im/escape-fix", label)
	require.Equal(t, StatusNeedsInput, kind)
}

func TestWorkerRemoteControl(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	RemoteControlWorkers = true
	defer func() { RemoteControlWorkers = false }()
	w, err := NewInstanceInFolder(SessionRequest{Title: "Escape в окне", Dir: t.TempDir()}, "/opt/homebrew/bin/claude")
	require.NoError(t, err)
	ws := filepath.Join(home, ".claude-squad", WorkerSettingsFile)
	require.Equal(t, "/opt/homebrew/bin/claude --remote-control 'Escape в окне' --settings '"+ws+"' --disallowedTools AskUserQuestion", w.Program)
	data, err := os.ReadFile(ws)
	require.NoError(t, err)
	require.Contains(t, string(data), `"Bash(git push *)"`)
	require.Contains(t, string(data), `"Bash(git push *--force*)"`)
	// the user's own edits are kept, a broken file is not passed on
	require.NoError(t, os.WriteFile(ws, []byte("{oops"), 0o644))
	w, _ = NewInstanceInFolder(SessionRequest{Title: "x", Dir: t.TempDir()}, "claude")
	require.NotContains(t, w.Program, "--settings")
	data, _ = os.ReadFile(ws)
	require.Equal(t, "{oops", string(data))
}

func TestCallsUser(t *testing.T) {
	c := &Instance{Status: Ready, Conductor: true}
	screen := "❯ [squad] Изменения с прошлой сводки: #6 — ждёт дирижёра\n\n● Разобрал #6.\n⚠ НУЖЕН ТЫ: #6 ждёт разрешения на git push — подтверди во вкладке\n────\n❯ \n────\n"
	c.applyPane(screen, false, false)
	require.True(t, c.CallsUserNow())
	label, kind := c.StatusLabel()
	require.Equal(t, "⚠ НУЖЕН ТЫ · #6 ждёт разрешения на git push — подтверди во вкладке", label)
	require.Equal(t, StatusNeedsInput, kind)
	c.CallsUserSeen = c.CallsUser // the user opened it
	require.False(t, c.CallsUserNow())
	w := &Instance{Status: Ready}
	w.applyPane(screen, false, false)
	require.Equal(t, "", w.CallsUser, "only the conductor and the assistant call the user this way")
	a := &Instance{Status: Ready, Assistant: true}
	a.applyPane("❯ что с #6?\n\n● #6 ждёт разрешения.\n⚠ НУЖЕН ТЫ\nподтверди push в #6\n────\n❯ \n────\n", false, false)
	require.Equal(t, "подтверди push в #6", a.CallsUser)
}

func TestCallsUserIgnoresToolOutputAndQuotes(t *testing.T) {
	list := "❯ [squad] сводка\n\n⏺ Bash(cs ctl list)\n  ⎿  #15 Помощник — ⚠ НУЖЕН ТЫ · зайди | main | /y\n     #2 Demo — ждёт дирижёра\n\n⏺ Всё спокойно.\n────\n❯ \n────\n"
	require.Equal(t, "", callsUser(currentTurn(list)), "another session's status in tool output")
	quote := "❯ ты написал «⚠ НУЖЕН ТЫ: доступ к B24» — дал доступ\n\n⏺ Спасибо, продолжаю.\n────\n❯ \n────\n"
	require.Equal(t, "", callsUser(currentTurn(quote)), "the user's message quoting it")
	own := "❯ [squad] сводка\n\n⏺ Разобрал.\n  ⚠ НУЖЕН ТЫ: #6 ждёт разрешения\n────\n❯ \n────\n"
	require.Equal(t, "#6 ждёт разрешения", callsUser(currentTurn(own)))
}

func TestMenuReviewFixes(t *testing.T) {
	// A question that happens to contain a permission phrase is still a question.
	q := " ☐ Release\nDo you want to create the release branch now?\n❯ 1. Yes (Recommended)\n  2. Not yet\n  3. Type something.\n────\n  4. Chat about this\nEnter to select · ↑/↓ to navigate · Esc to cancel\n"
	m, ok := ParseChoiceMenu(q)
	require.True(t, ok)
	i := &Instance{Status: Ready}
	i.applyPane(q, false, false)
	require.False(t, i.NeedsInput)
	require.NotNil(t, i.Menu)

	// A permission-granting dialog is never a menu to answer.
	bypass := " ☐ Plan\nReady to code?\n❯ 1. Yes, and bypass permissions\n  2. Yes, manually approve edits\n  3. No, keep planning\nEnter to select · ↑/↓ to navigate · Esc to cancel\n"
	i.applyPane(bypass, false, false)
	require.True(t, i.NeedsInput)
	require.Nil(t, i.Menu)

	// The description under an option counts for the risk check.
	m, ok = ParseChoiceMenu(" ☐ Next\nКак продолжить?\n❯ 1. Finish up (Recommended)\n     commit, push, open MR\n  2. Wait\nEnter to select · ↑/↓ to navigate · Esc to cancel\n")
	require.True(t, ok)
	require.Equal(t, "commit, push, open MR", m.Options[0].Desc)
	require.True(t, m.Risky())
	require.True(t, m.RiskyChoice(m.Options[0]))
	require.False(t, m.RiskyChoice(m.Options[1]))

	// A negation inside a risky label does not make it safe; one at the start does.
	push := ChoiceMenu{Question: "Что делаем?"}
	for _, l := range []string{"Force push, skip CI", "Да, удалить, не спрашивая", "Merge without waiting for review"} {
		require.True(t, push.RiskyChoice(MenuOption{Label: l}), l)
	}
	require.False(t, push.RiskyChoice(MenuOption{Label: "Пока не пушить"}))
	require.False(t, push.RiskyChoice(MenuOption{Label: "Не мержить"}))

	// Trust: only Claude Code's dialog itself at the bottom, not text mentioning it.
	require.False(t, isTrustDialog("diff: TrustMarkers = Quick safety check\n❯ \n"))
	require.True(t, isTrustDialog("Quick safety check: Is this a project you created or one you trust?\n❯ 1. Yes, I trust this folder\n  2. No, exit\n"))

	// The next question of the same menu starts its own wait.
	j := &Instance{Status: Ready}
	j.applyPane(" ☐ A  ☐ B  ✔ Submit\nFirst?\n❯ 1. x (Recommended)\n  2. y\nEnter to select · Tab/Arrow keys to navigate · Esc to cancel\n", false, false)
	j.MenuSince = j.MenuSince.Add(-20 * time.Minute)
	j.MenuEscalated = true
	j.applyPane(" ☒ A  ☐ B  ✔ Submit\nSecond?\n❯ 1. z (Recommended)\n  2. w\nEnter to select · Tab/Arrow keys to navigate · Esc to cancel\n", false, false)
	require.Less(t, time.Since(j.MenuSince), time.Minute)
	require.False(t, j.MenuEscalated)

	require.Equal(t, "curl ***/im.message.add", redactSecrets("curl /rest/1/abcdef123456/im.message.add"))
}

func TestTodoListIsNotAMenuChip(t *testing.T) {
	scr := "⏺ Update Todos\n  ⎿  ☐ Write tests\n     ☒ Fix parser\n\n" +
		"╭──╮\n│ Edit file │\n Do you want to make this edit to a.go?\n❯ 1. Yes\n  2. Yes, allow all edits during this session (shift+tab)\n  3. No, and tell Claude what to do differently (esc)\n" +
		"Enter to select · ↑/↓ to navigate · Esc to cancel\n"
	if _, ok := ParseChoiceMenu(scr); ok {
		t.Fatal("a permission prompt under a todo list parsed as a menu")
	}
	q := "  ⎿  ☐ Write tests\n ☐ Release\nWhich?\n❯ 1. A (Recommended)\n  2. B\nEnter to select · ↑/↓ to navigate · Esc to cancel\n"
	m, ok := ParseChoiceMenu(q)
	if !ok || len(m.Tabs) != 1 || m.Tabs[0] != "Release" || m.Question != "Which?" {
		t.Fatalf("menu under a todo list: %v %+v", ok, m)
	}
}

func TestPlainNo(t *testing.T) {
	for _, l := range []string{"No", "No, wait", "Пока не пушить", "Не пушить", "Skip", "Отменить", "Подождать", "Нет (Recommended)", "Don't push yet"} {
		require.True(t, isPlainNo(l), l)
	}
	for _, l := range []string{"Now push to origin", "Normal merge", "Nothing else, deploy", "Skip CI and force push", "Отменить ревью и удалить ветку", "Notify and push"} {
		require.False(t, isPlainNo(l), l)
	}
	m := ChoiceMenu{Question: "Что дальше?", Options: []MenuOption{{Number: 1, Label: "Now push to origin"}, {Number: 2, Label: "No"}}}
	require.True(t, m.RiskyChoice(m.Options[0]))
	require.False(t, m.RiskyChoice(m.Options[1]))
}

func TestPermissionLikeAllowAll(t *testing.T) {
	m := ChoiceMenu{Question: "Edit a.go?", Options: []MenuOption{{Number: 1, Label: "Yes"}, {Number: 2, Label: "Yes, allow all edits during this session (shift+tab)"}}}
	require.True(t, m.PermissionLike())
}

func TestOwnStatusline(t *testing.T) {
	require.True(t, ownStatuslineRe.MatchString("cs statusline"))
	require.True(t, ownStatuslineRe.MatchString("/Users/x/bin/cs statusline --pet tyan"))
	require.False(t, ownStatuslineRe.MatchString("~/.claude/statusline.sh"))
	require.False(t, ownStatuslineRe.MatchString("t39 statusline-plt"))
}

func TestCallsUserListItem(t *testing.T) {
	require.Equal(t, "#3 ждёт merge", callsUser("● Итог:\n  - ⚠ НУЖЕН ТЫ: #3 ждёт merge\n"))
	require.Equal(t, "#4 доступ", callsUser("● **⚠ НУЖЕН ТЫ:** #4 доступ\n"))
	require.Equal(t, "", callsUser("  ⎿  echo '⚠ НУЖЕН ТЫ: test'\n"))
}

func TestWorkerSettingsUpgrade(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ws := filepath.Join(home, ".claude-squad", WorkerSettingsFile)
	require.NoError(t, os.MkdirAll(filepath.Dir(ws), 0o700))
	require.NoError(t, os.WriteFile(ws, settingsJSON(legacyDefaults[0][0], legacyDefaults[0][1]), 0o600))
	EnsureWorkerSettings()
	data, _ := os.ReadFile(ws)
	require.Contains(t, string(data), `"Bash(rm -rf node_modules)"`)
	require.NotContains(t, string(data), `"Bash(rm *)"`)
	// an edited file stays as it is
	edited := strings.Replace(string(data), "git add)", "git add) ", 1)
	require.NoError(t, os.WriteFile(ws, []byte(edited), 0o600))
	EnsureWorkerSettings()
	data, _ = os.ReadFile(ws)
	require.Equal(t, edited, string(data))
}
