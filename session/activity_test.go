package session

import "testing"

const screenHints = "\x1b[1m● Да, обе спеки запушены.\x1b[0m\n\n  - BRIEF: ревизия 3\n\n* Baked for 23s · done 2:57 PM\n\n※ recap: Задача — ревизия подсказок по наведению: спека (ревизия 3) запушена и ждёт\n  утверждения Евгением, MR !19809 стоит в очереди ревью бота. Нужно твоё решение по\n  видео Евгения про цитирование: спросить его или завести отдельную задачу.\n                                          ✔ Update installed · Restart to apply\n────────────────────────────\n❯ \n────────────────────────────\n  ▸▸ auto mode on (shift+tab to cycle)\n"

func TestRecapAndStage(t *testing.T) {
	i := &Instance{Status: Ready}
	if !i.ApplyPaneContent(screenHints, false) {
		t.Fatal("recap not detected")
	}
	if got := i.Stage(); got != "ждёт твоего решения" {
		t.Fatalf("stage = %q (recap %q)", got, i.Recap)
	}
	if label, _ := i.StatusLabel(); label != "ждёт тебя" {
		t.Fatalf("label %q", label)
	}
	if len(i.TailLines) == 0 || i.TailLines[len(i.TailLines)-1] == "❯ " {
		t.Fatalf("tail should drop the input box: %q", i.TailLines)
	}
	cases := map[string]string{
		"Чиним падение; фикс в MR !15376. Следующий шаг — получить одобрение a.i.belkin и влить MR.": "ждёт одобрения",
		"Цель — довести MR !18889 до merge. Дальше жду ответа ревью-бота.":                           "ждёт ревью",
		"Все пять задач завершены. Следующий шаг: включить VPN, чтобы я проверил MR.":                "нет сети / VPN",
		"!18990 готов, но влить его могут только maintainer'ы":                                       "ждёт мержа",
	}
	for recap, want := range cases {
		if got := classifyRecap(recap); got != want {
			t.Errorf("%q -> %q, want %q", recap, got, want)
		}
	}
	p := &Instance{Status: Ready}
	p.ApplyPaneContent("Bash command\n rm -rf x\nDo you want to proceed?\n❯ 1. Yes\n  2. No, and tell Claude what to do differently\n", false)
	if label, _ := p.StatusLabel(); label != "нужно разрешение · rm -rf x" {
		t.Fatalf("permission label %q", label)
	}
}

func TestSuggestion(t *testing.T) {
	raw := "● done\n\x1b[2m────\x1b[0m\n❯ \x1b[2mfix MR, просто отправь ответ\x1b[0m\n────\n  auto mode on\n"
	if got := extractSuggestion(raw); got != "fix MR, просто отправь ответ" {
		t.Fatalf("got %q", got)
	}
	typed := "● done\n────\n❯ я сам это пишу\n────\n"
	if got := extractSuggestion(typed); got != "" {
		t.Fatalf("typed text treated as suggestion: %q", got)
	}
	empty := "● done\n────\n❯ \n────\n"
	if got := extractSuggestion(empty); got != "" {
		t.Fatalf("empty: %q", got)
	}
}

func TestSuggestionTrueColor(t *testing.T) {
	raw := "────\n❯ \x1b[38;2;153;153;153mзапусти тесты\x1b[39m\n────\n"
	if got := extractSuggestion(raw); got != "запусти тесты" {
		t.Fatalf("got %q", got)
	}
}

func TestDraft(t *testing.T) {
	raw := "● done\n────\n❯ начало черновика,\n  вторая строка\n────\n  auto mode on\n"
	if got := extractDraft(raw); got != "начало черновика,\nвторая строка" {
		t.Fatalf("draft %q", got)
	}
	if got := extractSuggestion(raw); got != "" {
		t.Fatalf("draft treated as suggestion: %q", got)
	}
	ghost := "────\n❯ \x1b[2mподсказка\x1b[0m\n────\n"
	if got := extractDraft(ghost); got != "" {
		t.Fatalf("suggestion treated as draft: %q", got)
	}
}

func TestStaleRecapAndNetwork(t *testing.T) {
	i := &Instance{Status: Ready}
	i.ApplyPaneUpdate("※ recap: Следующий шаг — дождаться ревью.\n────\n❯ \n────\n", false, false)
	if i.Stage() != "ждёт ревью" {
		t.Fatalf("stage %q", i.Stage())
	}
	// Agent starts working on something new; recap scrolled away.
	i.Status = Running
	i.ApplyPaneUpdate("● Читаю файлы...\n* Thinking…\n", false, true)
	i.Status = Ready
	if i.Stage() != "" {
		t.Fatalf("stale stage shown: %q", i.Stage())
	}
	i.ApplyPaneUpdate("Состояние MR посмотреть не удалось: git.example.com не открывается (no such host)\n────\n❯ \n────\n", false, false)
	if label, _ := i.StatusLabel(); label != "нет сети" {
		t.Fatalf("label %q", label)
	}
	if got := classifyRecap("Цель — довести MR !18889 до merge. Правка запушена, жду ответа ревью-бота."); got != "ждёт ревью" {
		t.Fatalf("goal sentence should not win: %q", got)
	}
}

func TestOldRecapBeforeNewMessage(t *testing.T) {
	screen := "※ recap: Пока вас не было, жду ответа ревью-бота.\n\n❯ возьми тикеты 260259-260\n\n● Работаю над тикетами\n  ⎿ Interrupted · What should Claude do instead?\n────\n❯ q\n────\n"
	i := &Instance{Status: Ready}
	i.ApplyPaneUpdate(screen, false, false)
	if i.Stage() != "" {
		t.Fatalf("old recap used: %q", i.Stage())
	}
	if l, _ := i.StatusLabel(); l != "прерван — ждёт указаний" {
		t.Fatalf("label %q", l)
	}
}

func TestRealClaudeInputBox(t *testing.T) {
	// Real capture: empty input box draws a dim "❯" followed by NBSP.
	empty := "────\n\x1b[2m❯ \n\x1b[0m\x1b[37m────\n"
	if d := extractDraft(empty); d != "" {
		t.Fatalf("empty box gave draft %q", d)
	}
	// Suggestion: dim carries over from the prompt sign, no new code before the text.
	sugg := "────\n\x1b[2m❯ коммить без прогона тестов, пуш\n\x1b[0m\x1b[37m────\n"
	if d := extractDraft(sugg); d != "" {
		t.Fatalf("suggestion treated as draft: %q", d)
	}
	if s := extractSuggestion(sugg); s != "коммить без прогона тестов, пуш" {
		t.Fatalf("suggestion not found: %q", s)
	}
	// Typed text: the style resets to normal before the text.
	typed := "────\n\x1b[2m❯ \x1b[0mосвободи машину\n\x1b[37m────\n"
	if d := extractDraft(typed); d != "освободи машину" {
		t.Fatalf("draft not found: %q", d)
	}
	typed2 := "────\n❯ освободи машину\n────\n"
	if d := extractDraft(typed2); d != "освободи машину" {
		t.Fatalf("plain draft not found: %q", d)
	}
}

func TestMenuIsNotInputBox(t *testing.T) {
	screen := "Bash command\n  rm x\nDo you want to proceed?\n❯ 1. Yes\n  2. Yes, and don't ask again\n  3. No, and tell Claude what to do differently\n"
	if d := extractDraft(screen); d != "" {
		t.Fatalf("menu read as draft: %q", d)
	}
	i := &Instance{Status: Ready}
	i.ApplyPaneContent(screen, false)
	if i.InputReady() {
		t.Fatal("menu counted as ready input")
	}
}
