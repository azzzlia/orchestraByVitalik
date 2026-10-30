package main

import (
	"claude-squad/config"
	"claude-squad/session"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var usageCalibrate []string

var usageCmd = &cobra.Command{
	Use:   "usage",
	Short: "Сколько токенов съели сессии и насколько заполнены лимиты (без токенов)",
	Long: "Считает по логам Claude Code. Проценты появятся после калибровки: открой /usage\n" +
		"в любой сессии Claude и передай, что там написано: cs usage --calibrate 5h=40 --calibrate week=25",
	RunE: func(cmd *cobra.Command, args []string) error {
		t := session.NewUsageTracker()
		t.Refresh()
		now := time.Now()
		_, u5 := t.Since(now.Add(-session.Window5h))
		_, uw := t.Since(now.Add(-session.WindowWeek))
		cfg := config.LoadConfig()
		if len(usageCalibrate) > 0 {
			for _, c := range usageCalibrate {
				k, v, ok := strings.Cut(c, "=")
				pct, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64)
				if !ok || err != nil || pct <= 0 || pct > 100 {
					return fmt.Errorf("не понял «%s»: нужно 5h=40 или week=25", c)
				}
				switch k {
				case "5h":
					if u5 == 0 {
						return fmt.Errorf("за последние 5 часов нет расхода — калибровать не по чему")
					}
					cfg.TokenBudget5h = u5 / (pct / 100)
				case "week", "7d":
					if uw == 0 {
						return fmt.Errorf("за неделю нет расхода — калибровать не по чему")
					}
					cfg.TokenBudgetWeek = uw / (pct / 100)
				default:
					return fmt.Errorf("окно «%s» не знаю: 5h или week", k)
				}
			}
			if err := config.SaveConfig(cfg); err != nil {
				return err
			}
			fmt.Println("сохранил; Squad подхватит после перезапуска")
		}
		pct := func(u, b float64) string {
			if b <= 0 {
				return "—"
			}
			return fmt.Sprintf("%.0f%%", 100*u/b)
		}
		fmt.Printf("За 5 часов: %s (%s лимита) · за неделю: %s (%s лимита)\n",
			session.FormatUnits(u5), pct(u5, cfg.TokenBudget5h), session.FormatUnits(uw), pct(uw, cfg.TokenBudgetWeek))

		midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		today, total := t.Since(midnight)
		names := map[string]string{}
		if snap, err := session.ReadStatusSnapshot(); err == nil {
			for _, s := range snap.Sessions {
				if s.ConvID != "" {
					names[s.ConvID] = fmt.Sprintf("#%d %s", s.Number, s.Title)
				}
			}
		}
		type row struct {
			name  string
			units float64
		}
		var rows []row
		other := 0.0
		for id, u := range today {
			if n, ok := names[id]; ok {
				rows = append(rows, row{n, u})
			} else {
				other += u
			}
		}
		sort.Slice(rows, func(a, b int) bool { return rows[a].units > rows[b].units })
		fmt.Printf("\nСегодня всего %s:\n", session.FormatUnits(total))
		for _, r := range rows {
			fmt.Printf("  %7s  %s\n", session.FormatUnits(r.units), r.name)
		}
		if other > 0 {
			fmt.Printf("  %7s  вне Squad (VS Code, терминал, сессии без id)\n", session.FormatUnits(other))
		}
		if cfg.TokenBudget5h <= 0 && len(usageCalibrate) == 0 {
			fmt.Println("\nЧтобы видеть проценты: открой /usage в Claude и выполни, например, cs usage --calibrate 5h=40 --calibrate week=25")
		}
		return nil
	},
}

func init() {
	usageCmd.Flags().StringArrayVar(&usageCalibrate, "calibrate", nil, "что показывает /usage: 5h=40, week=25")
	usageCmd.SilenceUsage = true
	rootCmd.AddCommand(usageCmd)
}
