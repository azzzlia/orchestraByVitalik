package app

import (
	"claude-squad/log"
	"claude-squad/session"
	"fmt"
	"strconv"
	"time"
)

// handleMenus deals with workers stuck on a choice menu (AskUserQuestion) that
// the conductor has not answered: after menu_auto_minutes Squad picks the
// "(Recommended)" option, or — for risky menus (push, merge, deletion…), menus
// without a recommendation and multi-choice ones — tells the user.
func (m *home) handleMenus(now time.Time) {
	wait := m.appConfig.MenuAuto()
	for _, inst := range m.list.GetInstances() {
		m.notifyLongPermission(inst, now)
		menu := inst.Menu
		if menu == nil || inst.Conductor || inst.Assistant || !session.ConductorActive || inst.Status != session.Ready {
			continue
		}
		key := session.MenuKey(*menu)
		if inst.MenuHandled == key || inst.MenuSince.IsZero() {
			continue
		}
		escalateAfter := wait
		if escalateAfter <= 0 {
			escalateAfter = 10 * time.Minute
		}
		if now.Sub(inst.MenuSince) < escalateAfter {
			continue
		}
		inst.MenuHandled = key
		rec, hasRec := menu.Recommended()
		// Only menus of workers the conductor opened, not marked for the user, and only
		// if the very same question is still on the screen right now.
		auto := wait > 0 && hasRec && !menu.Multi && !menu.Review && !menu.Risky() && inst != m.attached &&
			inst.ByConductor && inst.MarkedFor != "user"
		if auto {
			if fresh, ok := session.ParseChoiceMenu(inst.Screen()); !ok || session.MenuKey(fresh) != key || fresh.Risky() {
				auto = false
			}
		}
		if auto {
			if err := inst.PressKeys(strconv.Itoa(rec.Number)); err != nil {
				log.WarningLog.Printf("menu auto-pick %q: %v", inst.Title, err)
			} else {
				msg := fmt.Sprintf("#%d %s: дирижёр не ответил на меню за %d мин — выбрал рекомендованное «%s» (вопрос: %s)",
					inst.Number, inst.Title, int(escalateAfter.Minutes()), rec.Label, menu.Question)
				session.AppendEvent(session.Event{Kind: "menu", Number: inst.Number, Title: inst.Title, Text: msg})
				log.InfoLog.Print(msg)
				continue
			}
		}
		inst.MenuEscalated = true
		why := "нужен твой выбор"
		switch {
		case menu.Risky():
			why = "вопрос про push/merge/удаление — только ты"
		case !hasRec:
			why = "нет рекомендованного варианта"
		case menu.Multi:
			why = "можно выбрать несколько"
		}
		session.AppendEvent(session.Event{Kind: "menu", Number: inst.Number, Title: inst.Title, Text: "меню ждёт пользователя: " + menu.Summary()})
		notifyUser(fmt.Sprintf("#%d ждёт выбора в меню", inst.Number), menu.Question+" — "+why)
	}
}

// notifyLongPermission tells the user once about a worker's permission prompt that has
// waited 15 minutes (workers' own notifications are off; the conductor may be down).
func (m *home) notifyLongPermission(inst *session.Instance, now time.Time) {
	if !inst.NeedsInput || inst.Conductor || inst.Assistant || inst.NeedsSince.IsZero() || inst == m.attached {
		return
	}
	key := inst.PermissionCmd + "@" + inst.NeedsSince.Format(time.RFC3339)
	if inst.PermNotified == key || now.Sub(inst.NeedsSince) < 15*time.Minute {
		return
	}
	inst.PermNotified = key
	what := inst.PermissionCmd
	if what == "" || what == "?" {
		what = "разрешение"
	}
	session.AppendEvent(session.Event{Kind: "permission", Number: inst.Number, Title: inst.Title, Text: "ждёт разрешения 15 мин: " + what})
	notifyUser(fmt.Sprintf("#%d ждёт разрешения 15 мин", inst.Number), what)
}
