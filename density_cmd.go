package main

import (
	"claude-squad/session"
	"claude-squad/ui"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var densityAuto bool

func fmtDensity(v float64) string { return strings.Replace(fmt.Sprintf("%.1f", v), ".", ",", 1) }

var densityCmd = &cobra.Command{
	Use:   "density [значение]",
	Short: "Плотность для питомца: оценка по логам Claude; cs density 10,4 — задать на сегодня вручную",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if densityAuto {
			_ = session.ClearDensity()
			fmt.Println("ручное значение убрано — снова считаю по логам")
		}
		if len(args) == 1 {
			v, ok := session.ParseDensity(args[0])
			if !ok {
				return fmt.Errorf("не понял «%s»: нужно число, например 10,4", args[0])
			}
			if err := session.WriteDensity(v); err != nil {
				return err
			}
			fmt.Printf("на сегодня плотность %s — %s (завтра снова по логам)\n", fmtDensity(v), ui.PetMoodLabels[ui.PetMood(v)])
			return nil
		}
		if v, at, ok := session.ReadDensity(); ok && at.Format("2006-01-02") == time.Now().Format("2006-01-02") {
			fmt.Printf("задано вручную на сегодня: %s — %s (cs density --auto — вернуть оценку)\n", fmtDensity(v), ui.PetMoodLabels[ui.PetMood(v)])
		}
		t := session.NewDensityTracker()
		t.Refresh(time.Now())
		est := t.Estimate()
		if !est.HasActivity {
			fmt.Println("за последние сутки работы агентов в логах Claude нет")
			return nil
		}
		fmt.Printf("оценка по логам Claude за последние сутки: %s — %s\n", fmtDensity(est.Value), ui.PetMoodLabels[ui.PetMood(est.Value)])
		fmt.Printf("  работа с ИИ: %.1f ч, агенты в сумме: %.1f ч, веток (сессий и субагентов): %d\n", est.AIHours, est.AgentHours, est.Branches)
		fmt.Println("  это приближение формулы сервиса отчётов: Codex и Cursor не учитываются")
		return nil
	},
}

func init() {
	densityCmd.Flags().BoolVar(&densityAuto, "auto", false, "убрать ручное значение и считать по логам")
	densityCmd.SilenceUsage = true
	rootCmd.AddCommand(densityCmd)
}
