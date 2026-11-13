package session

import "time"

// ConductorActive is set by Squad each tick: a conductor session exists and gets
// digests, so a worker that finished a step waits for it, not for the user.
var ConductorActive bool

// ConductorGiveUp: a worker the conductor has not handled for this long is shown
// as waiting for the user ("дирижёр не разобрал").
const ConductorGiveUp = 45 * time.Minute

// ConductorTurns counts the conductor's finished turns. A worker counts as «not handled»
// only when the conductor finished at least two turns since it went idle — it really
// had the digest and did nothing — not because the conductor was asleep, busy for long
// or the Mac slept (then the clock alone made every idle worker «ждёт тебя»).
var ConductorTurns uint64

// idleLabel says whose turn it is when the agent is idle with an empty box.
func (i *Instance) idleLabel() (string, StatusKind) {
	decision := !i.RecapStale && classifyRecap(i.Recap) == "ждёт твоего решения"
	switch {
	case i.Conductor:
		if decision {
			return "ждёт тебя · нужно твоё решение", StatusWaiting
		}
		return "на связи", StatusStandby
	case i.Assistant:
		return "ответил", StatusWaiting
	case i.MarkedFor == "user":
		return withReason("ждёт тебя", i.MarkReason), StatusWaiting
	case i.MarkedFor == "wait":
		return withReason("в ожидании", i.MarkReason), StatusOnHold
	case decision:
		return "ждёт тебя · нужно твоё решение", StatusWaiting
	case i.DraftBlocks():
		return "ты печатаешь", StatusWaiting
	case ConductorActive && (i.IdleSince.IsZero() || time.Since(i.IdleSince) < ConductorGiveUp || ConductorTurns-i.IdleTurn < 2):
		return "ждёт дирижёра", StatusConductor
	case ConductorActive:
		return "ждёт тебя · дирижёр не разобрал", StatusWaiting
	}
	return "ждёт тебя", StatusWaiting
}

func withReason(label, reason string) string {
	if reason == "" {
		return label
	}
	return label + " · " + reason
}

// ClearMarkWhenWorking drops the conductor's mark once the agent works again.
func (i *Instance) ClearMarkWhenWorking() {
	if i.Status == Running && i.MarkedFor != "" {
		i.MarkedFor, i.MarkReason = "", ""
	}
}
