package app

import (
	"claude-squad/log"
	"claude-squad/session"
	"claude-squad/session/tmux"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// The work VPN drops and reconnects now and then. Agents caught mid-step either
// show a network error and stop, or hang on an MCP call whose connection died.
// Squad probes a host that is only reachable through the VPN; while it is down,
// it leaves the sessions alone (and does not wake the conductor); once it is back,
// it gets every session that got stuck on the outage going again.

var (
	// netGrace lets agents and MCP servers recover on their own first.
	netGrace = 45 * time.Second
	// netStuckAfter: a "working" session whose screen has not moved for this long
	// after the VPN came back is stuck on a dead connection.
	netStuckAfter = 3 * time.Minute
	// netGiveUp: stop tracking the outage after this long (longer than the MCP
	// call limit, so a call that times out is still picked up).
	netGiveUp = 20 * time.Minute
	// netEscWait: how long to wait for the input box after pressing Esc.
	netEscWait = 40 * time.Second
)

const netContinueMsg = "[squad] Сеть/VPN пропадала и вернулась. Продолжай задачу с того места, " +
	"где остановился: повтори шаг, который сорвался из-за сети. Если инструмент всё ещё недоступен — напиши об этом в recap."

// netProbe holds the VPN state seen by the prober goroutine.
type netProbe struct {
	mu        sync.Mutex
	known, up bool
	changedAt time.Time
}

func (p *netProbe) get() (known, up bool, changedAt time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.known, p.up, p.changedAt
}

func (p *netProbe) set(up bool, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.known || p.up != up {
		p.known, p.up, p.changedAt = true, up, now
	}
}

// netRecovery tracks one session through recovery after an outage.
type netRecovery struct {
	sig         string // screen signature when the VPN dropped
	moved       bool   // the screen changed since then (the step is not stuck)
	interrupted time.Time
}

// setupNetwork applies the MCP timeout to new sessions and starts the VPN prober.
func (m *home) setupNetwork() {
	if t := m.appConfig.MCPToolTimeout(); t > 0 && os.Getenv("MCP_TOOL_TIMEOUT") == "" {
		tmux.ExtraEnv = append(tmux.ExtraEnv, fmt.Sprintf("MCP_TOOL_TIMEOUT=%d", t*1000))
	}
	hosts := m.appConfig.VPNCheck
	if len(hosts) == 0 {
		return
	}
	go func() {
		fails := 0
		for {
			if probeAny(hosts, 4*time.Second) {
				fails = 0
				m.net.set(true, time.Now())
			} else if fails++; fails >= 2 { // one lost probe is not an outage
				m.net.set(false, time.Now())
			}
			select {
			case <-m.ctx.Done():
				return
			case <-time.After(10 * time.Second):
			}
		}
	}()
}

func probeAny(hosts []string, timeout time.Duration) bool {
	for _, h := range hosts {
		if !strings.Contains(h, ":") {
			h += ":443"
		}
		if c, err := net.DialTimeout("tcp", h, timeout); err == nil {
			_ = c.Close()
			return true
		}
	}
	return false
}

// vpnDown reports whether the VPN is known to be down right now.
func (m *home) vpnDown() bool {
	known, up, _ := m.net.get()
	return known && !up
}

// handleNetwork runs every monitor tick with m.mu held.
func (m *home) handleNetwork() {
	known, up, changedAt := m.net.get()
	if !known {
		return
	}
	if !up {
		if !m.netDown {
			m.netDown = true
			m.netRecover = map[*session.Instance]*netRecovery{}
			m.netActions = nil
			session.AppendEvent(session.Event{At: changedAt, Kind: "vpn_down"})
			notifyUser("orchestraByVitalik: VPN отвалился", "Сессии подождут. Когда сеть вернётся, оркестр сам их растормошит.")
		}
		// Remember how each session looked when the network went away (also those
		// that start during the outage).
		for _, inst := range m.list.GetInstances() {
			if _, ok := m.netRecover[inst]; !ok && inst.Started() && !inst.Paused() {
				m.netRecover[inst] = &netRecovery{sig: inst.TurnSignature()}
			}
		}
		return
	}
	if m.netDown {
		m.netDown = false
		m.netUpAt = changedAt
		session.AppendEvent(session.Event{At: changedAt, Kind: "vpn_up"})
	}
	if len(m.netRecover) > 0 {
		m.recoverAfterOutage(time.Now())
	}
}

// recoverAfterOutage gets sessions that got stuck on the outage going again.
func (m *home) recoverAfterOutage(now time.Time) {
	sinceUp := now.Sub(m.netUpAt)
	if sinceUp < netGrace {
		return
	}
	alive := map[*session.Instance]bool{}
	for _, inst := range m.list.GetInstances() {
		alive[inst] = true
	}
	for inst, rec := range m.netRecover {
		done, action := m.recoverOne(inst, rec, alive[inst], sinceUp, now)
		if action != "" {
			text := fmt.Sprintf("#%d %s", inst.Number, action)
			m.netActions = append(m.netActions, text)
			session.AppendEvent(session.Event{Kind: "recover", Number: inst.Number, Title: inst.Title, Text: text})
		}
		if done {
			delete(m.netRecover, inst)
		}
	}
	if sinceUp > netGiveUp {
		m.netRecover = map[*session.Instance]*netRecovery{}
	}
	if len(m.netRecover) == 0 && len(m.netActions) > 0 {
		notifyUser("orchestraByVitalik: VPN вернулся", strings.Join(m.netActions, "; "))
		m.netActions = nil
	}
}

// recoverOne decides what to do with one session; done=true stops tracking it.
func (m *home) recoverOne(inst *session.Instance, rec *netRecovery, alive bool, sinceUp time.Duration, now time.Time) (done bool, action string) {
	if !alive || !inst.Started() || inst.Paused() || inst.Status == session.Loading {
		return true, ""
	}
	if inst == m.attached {
		return true, "не трогал — ты в сессии"
	}
	netErr, mcpErr := inst.TurnNetworkError()

	if !rec.interrupted.IsZero() {
		// Esc sent: wait for the input box, then restart or nudge.
		if inst.Status == session.Running || time.Since(inst.IdleSince) < 3*time.Second || !inst.InputReady() {
			if now.Sub(rec.interrupted) > netEscWait {
				return true, "не ответил на Esc — посмотри сам"
			}
			return false, ""
		}
		return true, m.nudge(inst, true) // it hung on a dead connection: fresh MCP servers
	}

	// The monitor sees "working" only on ticks where the screen changed.
	working := inst.Status == session.Running || time.Since(inst.IdleSince) < 5*time.Second
	if working {
		if cur := inst.TurnSignature(); cur != rec.sig {
			rec.sig, rec.moved = cur, true // moving; keep watching in case it ends in an error
			return false, ""
		}
		// Only a step that has not moved since the drop and waits on the network
		// (an MCP call, a network error) is stuck; long local work is left alone.
		if rec.moved || inst.Status != session.Running || !inst.WaitsOnNetwork() || sinceUp < netStuckAfter {
			return false, ""
		}
		if inst.DraftBlocks() {
			return true, "завис, но в поле твой черновик — не трогал"
		}
		if err := inst.Interrupt(); err != nil {
			log.WarningLog.Printf("interrupt %q: %v", inst.Title, err)
			return true, ""
		}
		rec.interrupted = now
		return false, ""
	}

	// Waiting for input.
	if inst.NeedsInput || !netErr {
		return true, "" // permission prompts are the user's; no network error — nothing to do
	}
	if inst.DraftBlocks() {
		return true, "ошибка сети, но в поле твой черновик — не трогал"
	}
	return true, m.nudge(inst, mcpErr)
}

// nudge tells the agent to continue; with restart, first restarts it (same
// conversation) so its MCP servers connect again.
func (m *home) nudge(inst *session.Instance, restart bool) string {
	if inst.Conductor {
		restart = false // the conductor has no work MCP servers; a restart only costs tokens
	}
	if restart {
		if err := inst.RestartAgent(); err != nil {
			log.WarningLog.Printf("restart %q after outage: %v", inst.Title, err)
			return "не смог перезапустить: " + err.Error()
		}
		inst.PendingPrompt = netContinueMsg
		return "перезапустил (MCP заново) и попросил продолжить"
	}
	if !inst.InputReady() {
		return ""
	}
	if err := inst.ReplaceInput(netContinueMsg); err != nil {
		log.WarningLog.Printf("nudge %q after outage: %v", inst.Title, err)
		return "не смог написать: " + err.Error()
	}
	return "попросил продолжить"
}

// notifyUser shows a macOS notification (no-op elsewhere).
func notifyUser(title, msg string) {
	if runtime.GOOS != "darwin" {
		log.InfoLog.Printf("notify: %s — %s", title, msg)
		return
	}
	esc := func(s string) string {
		return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
	}
	script := fmt.Sprintf(`display notification "%s" with title "%s" sound name "Funk"`, esc(msg), esc(title))
	go func() { _ = exec.Command("osascript", "-e", script).Run() }()
}
