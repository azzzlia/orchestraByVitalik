package app

import (
	"strings"
	"testing"
	"time"

	"claude-squad/config"
	"claude-squad/ui"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/stretchr/testify/require"
)

func TestBuildBriefEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sp := spinner.New()
	h := &home{appConfig: config.DefaultConfig(), list: ui.NewList(&sp, false)}
	now := time.Date(2026, 10, 29, 9, 1, 0, 0, time.Local)
	text := h.buildBrief(now, now.Add(-14*time.Hour))
	require.True(t, strings.HasPrefix(text, "Оркестр, утро 29.10."), text)
	require.Contains(t, text, "Ждут тебя: никто")
	// no conductor: the brief is a Mac notification only, and only once a day
	h.maybeMorningBrief(now)
	require.Equal(t, "2026-10-29", h.briefDay)
}

func TestMorningBriefTime(t *testing.T) {
	c := config.DefaultConfig()
	hh, mm, ok := c.MorningBriefTime()
	require.True(t, ok)
	require.Equal(t, 9, hh)
	require.Equal(t, 0, mm)
	c.MorningBriefAt = "08:30"
	hh, mm, _ = c.MorningBriefTime()
	require.Equal(t, 8, hh)
	require.Equal(t, 30, mm)
	c.MorningBriefAt = "off"
	_, _, ok = c.MorningBriefTime()
	require.False(t, ok)
}
