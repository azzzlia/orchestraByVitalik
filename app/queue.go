package app

import (
	"claude-squad/log"
	"claude-squad/session"
	"fmt"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
)

// handleQueueKey handles keys on the «ждут тебя» screen (u).
func (m *home) handleQueueKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := session.UserQueue(m.list.GetInstances())
	if m.queueIdx >= len(items) {
		m.queueIdx = len(items) - 1
	}
	if m.queueIdx < 0 {
		m.queueIdx = 0
	}
	var it *session.QueueItem
	if m.queueIdx < len(items) {
		it = &items[m.queueIdx]
	}
	key := msg.String()
	switch key {
	case "u", "esc", "q":
		m.queue = false
		return m, tea.Batch(tea.WindowSize(), m.instanceChanged())
	case "up", "k":
		if m.queueIdx > 0 {
			m.queueIdx--
		}
		return m, nil
	case "down", "j":
		if m.queueIdx < len(items)-1 {
			m.queueIdx++
		}
		return m, nil
	}
	if it == nil {
		return m, nil
	}
	switch key {
	case "enter", "o":
		m.list.SetSelectedInstance(it.Index)
		m.queue = false
		m.instanceChanged()
		return m.handleKeyPress(msg)
	case "i":
		m.list.SetSelectedInstance(it.Index)
		return m, m.openReply(it.Inst)
	case "d":
		var conductor *session.Instance
		for _, inst := range m.list.GetInstances() {
			if inst.Conductor {
				conductor = inst
			}
		}
		if conductor == nil {
			return m, m.handleError(fmt.Errorf("дирижёра нет — ответь самой сессии клавишей i"))
		}
		if conductor == it.Inst {
			return m, m.openReply(conductor)
		}
		m.replyPrefill = fmt.Sprintf("по #%d «%s»: ", it.Inst.Number, it.Inst.Title)
		return m, m.openReply(conductor)
	case "y":
		if it.Kind != session.QueuePermission || !it.Inst.NeedsInput {
			return m, nil // the prompt is gone: "1" would land in the input box
		}
		// The user approves this one request, once ("1. Yes"), from the queue.
		if err := it.Inst.PressKeys("1"); err != nil {
			return m, m.handleError(err)
		}
		log.InfoLog.Printf("queue: user approved %q once: %s", it.Inst.Title, it.Inst.PermissionCmd)
		session.AppendEvent(session.Event{Kind: "user", Number: it.Inst.Number, Title: it.Inst.Title,
			Text: fmt.Sprintf("#%d: ты разрешил один раз — %s", it.Inst.Number, it.Inst.PermissionCmd)})
		return m, nil
	}
	if n, err := strconv.Atoi(key); err == nil && n >= 1 && n <= 9 && it.Answerable() {
		o, ok := it.Inst.Menu.Option(n)
		if !ok || o.Free || o.Chat {
			return m, m.handleError(fmt.Errorf("вариант %d так не выбрать — открой сессию (enter) или напиши ей (i)", n))
		}
		if err := it.Inst.PressKeys(strconv.Itoa(n)); err != nil {
			return m, m.handleError(err)
		}
		it.Inst.MenuHandled = session.MenuKey(*it.Inst.Menu)
		session.AppendEvent(session.Event{Kind: "user", Number: it.Inst.Number, Title: it.Inst.Title,
			Text: fmt.Sprintf("#%d: ты выбрал «%s»", it.Inst.Number, o.Label)})
		return m, nil
	}
	return m, nil
}
