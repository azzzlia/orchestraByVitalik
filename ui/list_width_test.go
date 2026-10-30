package ui

import (
	"strings"
	"testing"

	"claude-squad/log"
	"claude-squad/session"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
)

func TestListLongStatusKeepsWidth(t *testing.T) {
	log.Initialize(false)
	defer log.Close()
	sp := spinner.New()
	l := NewList(&sp, false)
	l.SetSize(40, 30)
	inst := &session.Instance{Title: "Escape в окне вложений", Status: session.Ready, Number: 23,
		MarkedFor: "user", MarkReason: strings.Repeat("разрешение: git push -u origin im/very-long-branch-name ", 4)}
	l.AddInstance(inst)()
	for _, line := range strings.Split(l.String(), "\n") {
		if w := lipgloss.Width(line); w > 40 {
			t.Fatalf("line wider than the list (%d): %q", w, session.StripANSI(line))
		}
	}
}
