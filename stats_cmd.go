package main

import (
	"claude-squad/session"
	"fmt"
	"sort"
	"time"

	"github.com/spf13/cobra"
)

var statsDays int

func fmtDuration(d time.Duration) string {
	if d >= time.Hour {
		return fmt.Sprintf("%dч %02dм", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dм", int(d.Minutes()))
}

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Статистика за неделю: кто сколько работал и сколько ждал тебя (без токенов)",
	RunE: func(cmd *cobra.Command, args []string) error {
		now := time.Now()
		since := now.AddDate(0, 0, -statsDays)
		events := session.ReadEvents(since)
		if len(events) == 0 {
			fmt.Println("Пока нет истории: Squad начинает её писать после обновления.")
			return nil
		}
		stats := session.ComputeStats(events, now)
		var list []*session.SessionStats
		var work, wait, hold time.Duration
		finished := 0
		for _, s := range stats {
			list = append(list, s)
			work += s.Working
			wait += s.WaitingUser
			hold += s.OnHold
			finished += s.Finished
		}
		sort.Slice(list, func(a, b int) bool { return list[a].Working > list[b].Working })
		fmt.Printf("С %s по сегодня (%d дн.)\n\n", since.Format("02.01"), statsDays)
		fmt.Printf("Агенты работали: %s · ждали тебя: %s · ждали сами (CI, таймеры): %s\n", fmtDuration(work), fmtDuration(wait), fmtDuration(hold))
		fmt.Printf("Шагов закончено (агент отдал тебе): %d · сессий: %d\n\n", finished, len(list))
		fmt.Println("По сессиям (работал / ждал тебя / дольше всего ждал / шагов):")
		for _, s := range list {
			fmt.Printf("  #%-3d %-32s %8s %8s %8s %4d\n", s.Number, s.Title, fmtDuration(s.Working), fmtDuration(s.WaitingUser), fmtDuration(s.LongestWait), s.Finished)
		}
		var outages int
		var down time.Duration
		var downAt time.Time
		for _, e := range events {
			switch e.Kind {
			case "vpn_down":
				outages++
				downAt = e.At
			case "vpn_up":
				if !downAt.IsZero() {
					down += e.At.Sub(downAt)
					downAt = time.Time{}
				}
			}
		}
		if outages > 0 {
			fmt.Printf("\nVPN отваливался %d раз, всего %s.\n", outages, fmtDuration(down))
		}
		t := session.NewUsageTracker()
		t.Refresh()
		fmt.Println("\nТокены по дням:")
		for d := 6; d >= 0; d-- {
			day := time.Date(now.Year(), now.Month(), now.Day()-d, 0, 0, 0, 0, now.Location())
			_, upto := t.Since(day)
			_, after := t.Since(day.AddDate(0, 0, 1))
			fmt.Printf("  %s  %s\n", day.Format("Mon 02.01"), session.FormatUnits(upto-after))
		}
		return nil
	},
}

func init() {
	statsCmd.Flags().IntVar(&statsDays, "days", 7, "за сколько дней")
	statsCmd.SilenceUsage = true
	rootCmd.AddCommand(statsCmd)
}
