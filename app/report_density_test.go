package app

import (
	"strings"
	"testing"
	"time"
)

func TestLastWorkday(t *testing.T) {
	mon := time.Date(2026, 10, 26, 9, 0, 0, 0, time.Local)
	if d := lastWorkday(mon); d.Weekday() != time.Friday || d.Day() != 23 {
		t.Fatalf("monday → %v", d)
	}
	tue := time.Date(2026, 10, 27, 9, 0, 0, 0, time.Local)
	if d := lastWorkday(tue); d.Day() != 26 {
		t.Fatalf("tuesday → %v", d)
	}
}

func TestFormatDensityDay(t *testing.T) {
	day := time.Date(2026, 10, 27, 0, 0, 0, 0, time.Local)
	line := formatDensityDay(day, 5, "5 now=0 hours=08,10,11,14,15,21\n")
	if !strings.HasPrefix(line, "Плотность за вт 27.10: 5 ч из 6") {
		t.Fatalf("%q", line)
	}
	if !strings.Contains(line, "пустые рабочие часы: 07, 09, 12, 13, 16, 17, 18, 19") {
		t.Fatalf("%q", line)
	}
	if line := formatDensityDay(day, 7, "7 now=0 hours=07,08,09,10,11,12,13"); !strings.Contains(line, "из 6 ✓") {
		t.Fatalf("%q", line)
	}
}
