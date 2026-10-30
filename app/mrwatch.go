package app

import (
	"claude-squad/config"
	"claude-squad/session"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// mrCheckEvery: how often the conductor's digest carries the MR round.
const mrCheckEvery = 30 * time.Minute

// mrNote is the «MR-обход» line of the digest: every session's MRs still to watch,
// at most once per mrCheckEvery. The conductor checks each one in GitLab (pipeline,
// new review comments, merged) and reports back with cs ctl mr.
func (m *home) mrNote(now time.Time) string {
	if now.Sub(m.lastMRNoteAt) < mrCheckEvery {
		return ""
	}
	var parts []string
	for _, inst := range m.list.GetInstances() {
		if inst.Conductor || inst.Assistant {
			continue
		}
		if open := inst.OpenMRs(); len(open) > 0 {
			parts = append(parts, fmt.Sprintf("#%d %s: %s", inst.Number, inst.Title, strings.Join(open, ", ")))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "MR-обход (CLAUDE.md «MR-обход»): " + strings.Join(parts, "; ")
}

// diskNote warns the conductor (at most hourly) when the disk is getting full, and
// the user when it is nearly full: clones of the environment take gigabytes.
func (m *home) diskNote(now time.Time) string {
	if now.Sub(m.lastDiskNoteAt) < time.Hour {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	free := session.DiskFreeGB(home)
	if free < 0 || free >= session.DiskLowGB {
		return ""
	}
	m.lastDiskNoteAt = now
	if free < session.DiskStopGB {
		notifyUser("orchestraByVitalik: диск почти полон", fmt.Sprintf("Свободно %.0f ГБ — новые клоны и сессии не запускаются, нужна уборка (cs cleanup / старые bx-клоны).", free))
		return fmt.Sprintf("⚠ ДИСК: свободно %.0f ГБ — не запускай новых клонов и сессий; предложи пользователю уборку (сам не удаляй)", free)
	}
	return fmt.Sprintf("ДИСК: свободно %.0f ГБ — не больше одного нового клона окружения (bx clone ~4 ГБ), сначала cs ctl capacity", free)
}

// Version is Squad's version (set by main).
var Version string

func versionStampPath() string {
	dir, err := config.GetConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "last-version")
}

// updateNote tells the conductor, once, that Squad was updated: its CLAUDE.md may
// have new rules. Only the conductor gets it (not the assistant, not workers).
func (m *home) updateNote() string {
	if Version == "" || m.updateNoted {
		return ""
	}
	prev := ""
	if data, err := os.ReadFile(versionStampPath()); err == nil {
		prev = strings.TrimSpace(string(data))
	}
	if prev == Version {
		m.updateNoted = true
		return ""
	}
	if prev == "" {
		prev = "прошлой версии"
	}
	return fmt.Sprintf("Squad обновлён: %s → %s. Твой CLAUDE.md обновлён вместе с ним — перечитай его (новое — в CHANGELOG.md репы Squad, раздел %s) и работай по новым правилам", prev, Version, Version)
}

// updateNoteSent records that the conductor got the update note.
func (m *home) updateNoteSent() {
	m.updateNoted = true
	_ = os.WriteFile(versionStampPath(), []byte(Version), 0600)
}
