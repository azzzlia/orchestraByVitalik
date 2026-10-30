package main

import (
	"claude-squad/session"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var ctlUserOK bool

var ctlAnswerCmd = &cobra.Command{
	Use:   "answer <номер> <вариант|recommended|cancel|свой текст>",
	Short: "Ответить на меню выбора в сессии (вопрос агента с вариантами 1. 2. 3.)",
	Long: `Отвечает на меню выбора (AskUserQuestion) в сессии воркера:
  cs ctl answer 20 1             — вариант 1
  cs ctl answer 20 recommended   — вариант с пометкой (Recommended)
  cs ctl answer 20 1,3           — несколько, если в меню можно выбрать несколько
  cs ctl answer 20 "своими словами" — через «Type something»
  cs ctl answer 20 cancel        — закрыть меню (агент спросит текстом)
Варианты про push, merge, удаление и т. п. — только после «да» пользователя, с --user-ok.`,
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		snap, err := loadSnapshot()
		if err != nil {
			return err
		}
		e, err := findSession(snap, args[0])
		if err != nil {
			return err
		}
		if e.Assistant || e.Conductor {
			return fmt.Errorf("#%d — дирижёр/помощник: в их меню выбирает только пользователь", e.Number)
		}
		name := tmuxName(e)
		raw, err := capture(name, false)
		if err != nil {
			return err
		}
		menu, ok := session.ParseChoiceMenu(raw)
		if ok && menu.PermissionLike() {
			return fmt.Errorf("в #%d диалог разрешений («%s») — его подтверждает только пользователь", e.Number, menu.Question)
		}
		if !ok {
			return fmt.Errorf("в #%d нет открытого меню выбора (статус: %s) — для сообщения используй cs ctl send", e.Number, e.Status)
		}
		return answerMenu(e, name, menu, strings.Join(args[1:], " "))
	},
}

func init() {
	ctlAnswerCmd.SilenceUsage, ctlAnswerCmd.SilenceErrors = true, true
	ctlAnswerCmd.Flags().BoolVar(&ctlUserOK, "user-ok", false, "пользователь сказал «да» на этот вариант (push, merge, удаление…)")
	ctlCmd.AddCommand(ctlAnswerCmd)
}

func tmuxKeys(target string, literal bool, keys ...string) error {
	args := []string{"send-keys", "-t", target}
	if literal {
		args = append(args, "-l")
	}
	return exec.Command("tmux", append(args, keys...)...).Run()
}

// answerMenu presses the keys that answer menu with answer and reports what happened.
func answerMenu(e *session.StatusEntry, name string, menu session.ChoiceMenu, answer string) error {
	target := "=" + name + ":"
	answer = strings.TrimSpace(answer)
	low := strings.ToLower(answer)
	risky := func(o session.MenuOption) error {
		if menu.RiskyChoice(o) && !ctlUserOK {
			return fmt.Errorf("«%s» в #%d — это про push/merge/удаление или отправку: выбирай только после «да» пользователя (тогда повтори с --user-ok). Вопрос: %s",
				o.Label, e.Number, menu.Summary())
		}
		return nil
	}
	var chosen string
	switch {
	case low == "cancel" || low == "esc" || low == "отмена":
		if err := tmuxKeys(target, false, "Escape"); err != nil {
			return err
		}
		chosen = "отмена меню (агент получит отказ и спросит текстом)"
	case menu.Review:
		var n int
		switch {
		case low == "submit" || low == "1" || strings.HasPrefix(low, "отправ"):
			n = 1
		case low == "2" || strings.HasPrefix(low, "отмен"):
			n = 2
		default:
			return fmt.Errorf("в #%d экран проверки ответов: cs ctl answer %d submit — отправить, cancel — отменить", e.Number, e.Number)
		}
		o, _ := menu.Option(n)
		if err := tmuxKeys(target, true, strconv.Itoa(n)); err != nil {
			return err
		}
		chosen = o.Label
	case menu.Multi:
		var picked []string
		for _, f := range strings.FieldsFunc(answer, func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
			n, err := strconv.Atoi(f)
			o, ok := menu.Option(n)
			if err != nil || !ok || o.Chat || o.Free {
				return fmt.Errorf("в этом меню можно выбрать несколько: перечисли номера через запятую (%s)", menu.Summary())
			}
			if err := risky(o); err != nil {
				return err
			}
			picked = append(picked, o.Label)
			if o.Checked {
				continue
			}
			if err := tmuxKeys(target, true, strconv.Itoa(n)); err != nil {
				return err
			}
			time.Sleep(200 * time.Millisecond)
		}
		// Next question, or the review screen after the last one.
		if err := tmuxKeys(target, false, "Right"); err != nil {
			return err
		}
		chosen = strings.Join(picked, ", ")
	default:
		var o session.MenuOption
		ok := false
		if low == "recommended" || low == "r" || strings.HasPrefix(low, "рекоменд") {
			if o, ok = menu.Recommended(); !ok {
				return fmt.Errorf("в меню #%d нет рекомендованного варианта: %s", e.Number, menu.Summary())
			}
		} else if n, err := strconv.Atoi(strings.TrimSuffix(answer, ".")); err == nil {
			if o, ok = menu.Option(n); !ok {
				return fmt.Errorf("в меню #%d нет варианта %d: %s", e.Number, n, menu.Summary())
			}
		}
		if ok && o.Chat {
			return fmt.Errorf("«Chat about this» не выбирай — ответь вариантом или своим текстом")
		}
		if ok && o.Free {
			return fmt.Errorf("вариант %d — «%s»: передай сам текст, например cs ctl answer %d \"мой ответ\"", o.Number, o.Label, e.Number)
		}
		if ok {
			if err := risky(o); err != nil {
				return err
			}
			if err := tmuxKeys(target, true, strconv.Itoa(o.Number)); err != nil {
				return err
			}
			chosen = o.Label
			break
		}
		free, has := menu.FreeOption()
		if !has {
			return fmt.Errorf("в меню #%d нет поля для своего ответа — выбери номер: %s", e.Number, menu.Summary())
		}
		if IsRiskyText(answer) && !ctlUserOK {
			return fmt.Errorf("ответ про push/merge/удаление — только после «да» пользователя (тогда с --user-ok)")
		}
		if err := tmuxKeys(target, true, strconv.Itoa(free.Number)); err != nil {
			return err
		}
		time.Sleep(400 * time.Millisecond)
		if err := tmuxKeys(target, true, strings.ReplaceAll(answer, "\n", " ")); err != nil {
			return err
		}
		time.Sleep(300 * time.Millisecond)
		if err := tmuxKeys(target, false, "Enter"); err != nil {
			return err
		}
		chosen = "свой ответ «" + answer + "»"
	}
	// Check the menu reacted: it is gone, or the next question is shown (ticking
	// boxes in a multi-choice menu is not enough: the question must change).
	before := menuSig(menu)
	for k := 0; k < 15; k++ {
		time.Sleep(200 * time.Millisecond)
		raw, err := capture(name, false)
		if err != nil {
			return err
		}
		next, still := session.ParseChoiceMenu(raw)
		if !still {
			fmt.Printf("ответил в #%d %s: %s\n", e.Number, e.Title, chosen)
			return nil
		}
		if menuSig(next) != before {
			if next.Review {
				fmt.Printf("ответил в #%d: %s. Дальше экран проверки ответов — отправить: cs ctl answer %d submit\n", e.Number, chosen, e.Number)
			} else {
				fmt.Printf("ответил в #%d: %s. Следующий вопрос: %s\n", e.Number, chosen, next.Summary())
			}
			return nil
		}
	}
	return fmt.Errorf("#%d: меню не отреагировало на ответ — НЕ выбрано%s", e.Number, dumpScreen(name, "menu-stuck"))
}

func menuSig(m session.ChoiceMenu) string {
	s := m.Question + "|" + strconv.FormatBool(m.Review)
	for _, o := range m.Options {
		s += "|" + o.Label
	}
	return s
}

// IsRiskyText is session.IsRisky for a free-text answer.
func IsRiskyText(s string) bool { return session.IsRisky(s) }
