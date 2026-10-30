package session

import (
	"strings"
	"testing"
)

const transcript = `● Тут всё закрыто: все пять задач завершены.

❯ первый старый вопрос

● старый ответ

❯ запуш спеки?

❯ запушил спеки?

  Ran 1 shell command

● Да, обе спеки запушены, сейчас перепроверил.

  - BRIEF: ревизия 3, ждёт утверждения Евгением.
  - SDD: frontend

* Baked for 23s · done 2:57 PM

※ recap: Задача — ревизия подсказок.
                                   ✔ Update installed · Restart to apply
────────────────────────────
❯ 
────────────────────────────
  auto mode on
`

func TestParseExchange(t *testing.T) {
	got := parseExchange(transcript, 2, 40)
	var b strings.Builder
	for _, l := range got {
		if l.User {
			b.WriteString("U| ")
		} else {
			b.WriteString("A| ")
		}
		b.WriteString(l.Text + "\n")
	}
	s := b.String()
	t.Log("\n" + s)
	for _, want := range []string{"U| ❯ запуш спеки?", "U| ❯ запушил спеки?", "A| ● Да, обе спеки запушены", "recap: Задача"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q", want)
		}
	}
	for _, bad := range []string{"старый", "Ran 1", "Baked for", "Update installed", "auto mode"} {
		if strings.Contains(s, bad) {
			t.Fatalf("should not contain %q", bad)
		}
	}
}

func TestParseExchangeNBSP(t *testing.T) {
	raw := "● old\n\n❯ мой вопрос\n\n● ответ Claude\n\n────\n❯ \n────\n"
	got := parseExchange(raw, 2, 40)
	if len(got) == 0 || !got[0].User || got[len(got)-1].Text != "● ответ Claude" {
		t.Fatalf("%+v", got)
	}
}
