package app

import (
	"claude-squad/config"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVerLess(t *testing.T) {
	if !verLess("26.900.0", "26.1700.0") || verLess("26.1800.0", "26.1700.0") || verLess("26.1700.0", "26.1700.0") {
		t.Fatal("сравнение версий")
	}
}

func TestUpdArea(t *testing.T) {
	cases := map[string]string{
		"im/install/js/im/v2/component/list/src/a.js":      "js:component/list",
		"im/install/js/im/v2/component/list/dist/a.js":     "#dist",
		"im/lib/V2/Folder/Service.php":                     "php:V2/Folder",
		"im/tests/codeception/Unit/A.php":                  "#тесты",
		"im/install/js/im/v2/lib/x/test/a.test.js":         "#тесты",
		"im/dev/updates/26/26.1700.0/updater.php":          "апдейтер",
		"im/lang/ru/lib/a.php":                             "#переводы",
		"im/.ai/knowledge/a.md":                            "#docs",
		"intranet/lib/a.php":                               "~intranet",
		"im/install/js/im/v2/application/integration/a.ts": "js:application/integration",
	}
	for f, want := range cases {
		if got := updArea("im", f); got != want {
			t.Errorf("%s → %s, ждали %s", f, got, want)
		}
	}
}

func TestLinkTasks(t *testing.T) {
	got := linkTasks("Б24 782059 и M 0261560, уже [Б24 700001](x), хеш 1d2358678cdd")
	if !strings.Contains(got, "[Б24 782059](https://tracker.example.com/task/782059/)") ||
		!strings.Contains(got, "[M 261560](https://bugs.example.com/view.php?id=261560)") ||
		strings.Contains(got, "[[Б24 700001]") {
		t.Fatal(got)
	}
}

func TestTaskIDs(t *testing.T) {
	ids := taskIDs("im: 782059: (docs) QA; im:immobile:0260701: фикс; версия 26.1700.0; 123456")
	if strings.Join(ids, ",") != "782059,260701" {
		t.Fatal(ids)
	}
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command(args[0], args[1:]...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Тестер", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=Тестер", "GIT_COMMITTER_EMAIL=t@t")
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commitFile(t *testing.T, dir, file, msg string) {
	t.Helper()
	p := filepath.Join(dir, file)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	f.WriteString(msg + "\n")
	f.Close()
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-qm", msg)
}

// Полный путь: ветки в «origin», объявление в чате, разбор, долитый коммит, «Дополнить».
func TestUpdatesDigestFlow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	origin := filepath.Join(home, "origin")
	run(t, home, "git", "init", "-q", "-b", "master", origin)
	run(t, origin, "git", "config", "user.name", "Тестер")
	commitFile(t, origin, "im/lib/V2/Chat/a.php", "im: начало")
	run(t, origin, "git", "branch", "update/im/26.100.0")
	// MR в master
	run(t, origin, "git", "checkout", "-qb", "im/782059-sound")
	commitFile(t, origin, "im/install/js/im/v2/lib/sound-notification/src/a.js", "im: 782059: (fix) звук")
	commitFile(t, origin, "im/tests/codeception/Unit/SoundTest.php", "im: 782059: (test) звук")
	run(t, origin, "git", "checkout", "-q", "master")
	run(t, origin, "git", "merge", "-q", "--no-ff", "im/782059-sound", "-m", "im: 782059: входящий звук (merge branch 'im/782059-sound' into 'master')")
	// MR только с тестами
	run(t, origin, "git", "checkout", "-qb", "im/tests")
	commitFile(t, origin, "im/tests/codeception/Unit/X.php", "im: (test) x")
	run(t, origin, "git", "checkout", "-q", "master")
	run(t, origin, "git", "merge", "-q", "--no-ff", "im/tests", "-m", "im: (test) тесты (merge branch 'im/tests')")
	commitFile(t, origin, "im/install/js/im/v2/component/textarea/src/b.js", "im: 261560: (fix) правка при смене чата")
	run(t, origin, "git", "branch", "update/im/26.200.0")
	commitFile(t, origin, "im/lib/V2/Chat/a.php", "im: после среза в master — не должно попасть")
	run(t, origin, "git", "checkout", "-q", "update/im/26.200.0")
	commitFile(t, origin, "im/install/js/im/v2/component/search-results/src/c.js", "im: 781641: сортировка по дате")
	run(t, origin, "git", "checkout", "-q", "master")

	repo := filepath.Join(home, "devRepo", "modules")
	run(t, home, "git", "clone", "-q", origin, repo)
	run(t, repo, "git", "config", "user.name", "Тестер")

	defer func(f func(*updater) ([]chatMsg, string)) { updChatMsgs = f }(updChatMsgs)
	updChatMsgs = func(*updater) ([]chatMsg, string) {
		return []chatMsg{{at: time.Now(), text: "[b]Забрали в тест[/b] im 26.200.0, immobile 26.300.0"}, {at: time.Now(), text: "im 26.100.0 вышло"}}, ""
	}
	var prompts []string
	defer func(f func(*updater, string) (string, error)) { updRunModel = f }(updRunModel)
	updRunModel = func(_ *updater, p string) (string, error) {
		prompts = append(prompts, p)
		if strings.Contains(p, "Прошлый дайджест") {
			return "## Дополнение — новых изменений: 1\n- фикс сортировки — M 0262069", nil
		}
		return "## Касается #team\n| Задача | Что | QA |\n|---|---|---|\n| Б24 782059 | звук | есть |\n\n## Топ-5 зон риска\n**1. Поиск**", nil
	}
	notes := 0
	defer func(f func(string, string)) { notifyUser = f }(notifyUser)
	notifyUser = func(string, string) { notes++ }

	u := newUpdater(&config.Config{})
	u.scan(true)
	e := u.E["im/26.200.0"]
	if e == nil || !e.InTest || !strings.Contains(e.TestNote, "Забрали в тест") {
		t.Fatalf("обновление в тесте не распознано: %+v", u.E)
	}
	if u.E["im/26.100.0"] == nil || u.E["im/26.100.0"].InTest {
		t.Fatalf("26.100.0 не в тесте: %+v", u.E["im/26.100.0"])
	}
	u.digest("im/26.200.0")
	if e.Status != "" {
		t.Fatalf("разбор: %s %s", e.Status, e.Err)
	}
	p := prompts[0]
	for _, want := range []string{"MR|", "782059: входящий звук", "Без кода продукта (только тесты, доки, переводы, сборка): тесты 1", "commit|", "261560", "ПОСЛЕ_СРЕЗА|", "781641", "Задачи, где есть коммиты пользователя (Тестер"} {
		if !strings.Contains(p, want) {
			t.Errorf("в запросе к модели нет %q", want)
		}
	}
	if strings.Contains(p, "не должно попасть") || strings.Contains(p, "im: начало") {
		t.Error("в разбор попали чужие коммиты")
	}
	b, _ := os.ReadFile(u.digestPath("im/26.200.0"))
	d := string(b)
	for _, want := range []string{"# Обновление im 26.200.0", "update/im/26.100.0", "| Коммиты | 5 ", "1 MR из 2", "[Б24 782059](https://tracker.example.com"} {
		if !strings.Contains(d, want) {
			t.Errorf("в дайджесте нет %q\n%s", want, d)
		}
	}
	if notes != 1 {
		t.Errorf("уведомлений %d", notes)
	}

	// долили коммит → «+1», дополнение только по нему
	run(t, origin, "git", "checkout", "-q", "update/im/26.200.0")
	commitFile(t, origin, "im/install/js/im/v2/component/search-results/src/c.js", "im: 262069: (fix) сортировка")
	run(t, origin, "git", "checkout", "-q", "master")
	u.scan(true)
	if e.New != 1 {
		t.Fatalf("новых коммитов %d", e.New)
	}
	u.digest("im/26.200.0")
	if len(prompts) != 2 || strings.Contains(prompts[1], "781641: сортировка по дате|") || !strings.Contains(prompts[1], "262069") {
		t.Fatalf("дополнение: %v", prompts[len(prompts)-1])
	}
	b, _ = os.ReadFile(u.digestPath("im/26.200.0"))
	if !strings.Contains(string(b), "# Обновление im 26.200.0") || !strings.Contains(string(b), "[M 262069](https://bugs.example.com/view.php?id=262069)") {
		t.Fatalf("после дополнения:\n%s", b)
	}
	if e.New != 0 {
		t.Fatal("счётчик новых не сброшен")
	}
	// повторный разбор без новых коммитов — модель не зовём
	u.digest("im/26.200.0")
	if len(prompts) != 2 {
		t.Fatal("модель вызвана без новых коммитов")
	}
	// состояние переживает перезапуск
	u2 := newUpdater(&config.Config{})
	if u2.E["im/26.200.0"] == nil || u2.E["im/26.200.0"].DoneHead == "" {
		t.Fatal("состояние не сохранилось")
	}
	l := u2.list()["items"].([]updView)
	if len(l) == 0 || l[0].Key != "im/26.200.0" || !l[0].Digest {
		t.Fatalf("список: %+v", l)
	}
}
