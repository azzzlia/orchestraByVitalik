package app

import (
	"strings"
	"testing"
)

func TestLastQuestion(t *testing.T) {
	ai := "Четыре строки в очереди.\n\n**Ночные e2e 12.11.** Все три потока ещё идут.\n\nВопрос: дорабатывать разбор, когда прогон дойдёт?\n1. Вернуться к нему ближе к 09:30 UTC. Рекомендую этот вариант.\n2. Не ждать: следующая ночь покажет то же.\n\nИтог: блокеров нет."
	q := lastQuestion([]tBlock{{kind: "you", text: "добавь строки"}, {kind: "ai", text: ai}})
	if !strings.HasPrefix(q, "Вопрос: дорабатывать разбор") || !strings.Contains(q, "1. Вернуться") || !strings.Contains(q, "2. Не ждать") || strings.Contains(q, "Итог") {
		t.Fatalf("%q", q)
	}
	if lastQuestion([]tBlock{{kind: "ai", text: ai}, {kind: "you", text: "1"}}) != "" {
		t.Fatal("после ответа пользователя вопроса нет")
	}
	if lastQuestion([]tBlock{{kind: "ai", text: "Готово, всё прошло."}}) != "" {
		t.Fatal("нет вопроса")
	}
	if q := lastQuestion([]tBlock{{kind: "ai", text: "Сделал.\nКоммитить сейчас или утром?"}, {kind: "tool", text: "Bash(x)"}}); q != "Коммитить сейчас или утром?" {
		t.Fatalf("%q", q)
	}
}
