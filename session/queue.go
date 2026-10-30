package session

import (
	"sort"
	"time"
)

// The «ждут тебя» queue: every session that cannot go on without the user, most
// urgent first, with what exactly it needs.

// QueueKind is what a session needs from the user.
type QueueKind int

const (
	QueueCalls      QueueKind = iota // the conductor or the assistant calls: «⚠ НУЖЕН ТЫ»
	QueuePermission                  // a permission prompt (only the user answers it)
	QueueTrust                       // "do you trust this folder?"
	QueueMenu                        // a choice menu the conductor may not answer
	QueueDecision                    // the conductor marked it: "ждёт тебя · …"
	QueueWaiting                     // idle and its turn is the user's
)

// QueueItem is one row of the queue.
type QueueItem struct {
	Index int // in the instance list
	Inst  *Instance
	Kind  QueueKind
	Text  string // the reason, the command, the menu in one line
	Since time.Time
}

// KindLabel is the short tag shown in front of the row.
func (q QueueItem) KindLabel() string {
	switch q.Kind {
	case QueueCalls:
		return "зовёт"
	case QueuePermission:
		return "разрешение"
	case QueueTrust:
		return "доверие"
	case QueueMenu:
		return "меню"
	case QueueDecision:
		return "решение"
	}
	return "ждёт"
}

// Answerable: single-choice menu options can be picked from the queue with a digit.
func (q QueueItem) Answerable() bool {
	return q.Kind == QueueMenu && q.Inst.Menu != nil && !q.Inst.Menu.Multi && !q.Inst.Menu.Review
}

// UserQueue lists the sessions waiting for the user.
func UserQueue(instances []*Instance) []QueueItem {
	var out []QueueItem
	for idx, inst := range instances {
		if inst == nil || !inst.Started() || inst.Paused() {
			continue
		}
		it := QueueItem{Index: idx, Inst: inst, Since: inst.IdleSince}
		label, kind := inst.StatusLabel()
		switch {
		case inst.CallsUserNow():
			it.Kind, it.Text = QueueCalls, inst.CallsUser
		case inst.TrustPrompt:
			it.Kind, it.Text = QueueTrust, "подтверди доверие к папке"
		case kind == StatusNeedsInput && inst.Menu == nil:
			it.Kind, it.Text = QueuePermission, inst.PermissionCmd
			if !inst.NeedsSince.IsZero() {
				it.Since = inst.NeedsSince
			}
		case inst.Menu != nil && kind != StatusConductor:
			// a menu that is the user's (not «ждёт дирижёра»)
			it.Kind, it.Text = QueueMenu, inst.Menu.Summary()
			if !inst.MenuSince.IsZero() {
				it.Since = inst.MenuSince
			}
		case inst.MarkedFor == "user":
			it.Kind, it.Text = QueueDecision, inst.MarkReason
		case inst.Assistant:
			continue // "ответил" is not a call; the assistant calls with ⚠ НУЖЕН ТЫ
		case kind == StatusWaiting || kind == StatusNeedsInput:
			it.Kind, it.Text = QueueWaiting, label
			if inst.Recap != "" && !inst.RecapStale {
				it.Text = label + " — " + inst.Recap
			}
		default:
			continue
		}
		if it.Text == "" {
			it.Text = label
		}
		out = append(out, it)
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Kind != out[b].Kind {
			return out[a].Kind < out[b].Kind
		}
		return out[a].Since.Before(out[b].Since) // waiting longest first
	})
	return out
}
