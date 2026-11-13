package main

import (
	"claude-squad/config"
	"claude-squad/session"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// `cs statusline` prints the density score for Claude Code's
// status line (under the input box). `--install` puts it into ~/.claude/settings.json.

var (
	statuslineInstall   bool
	statuslineUninstall bool
)

func fmtScore(v float64) string { return strings.Replace(fmt.Sprintf("%.1f", v), ".", ",", 1) }

// parColor: the zones of parallel hours (emerald from 15).
func parColor(v float64) string {
	switch {
	case v >= 15:
		return "1;32"
	case v >= 10:
		return "32"
	case v >= 6:
		return "33"
	default:
		return "31"
	}
}

func scoreColor(v float64) string {
	switch {
	case v >= 8:
		return "32"
	case v >= 6:
		return "33"
	default:
		return "31"
	}
}

func statusLineText() string {
	// Only caches Squad keeps fresh: this runs on every Claude Code redraw, so no log
	// scans and no t39 command here.
	v, ok := session.CachedDensity(30 * time.Minute)
	cfg := config.LoadConfig()
	label, color := "плт", scoreColor
	if cfg.ParallelScore() {
		label, color = "пар", parColor // parallel hours today: agent sessions + hours with commits
	}
	t39 := ""
	if tv, tok := session.CachedT39(); tok {
		t39 = fmt.Sprintf(" \x1b[2m· t39\x1b[0m \x1b[%sm%s\x1b[0m", scoreColor(tv), fmtScore(tv))
	}
	if !ok {
		return "\x1b[2m" + label + " —\x1b[0m" + t39
	}
	return fmt.Sprintf("%s \x1b[%sm%s\x1b[0m%s", label, color(v), fmtScore(v), t39)
}

func claudeSettingsPath() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "settings.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "settings.json")
}

func statusLineBackupPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude-squad", "statusline-backup.json")
}

func writeJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_ = os.MkdirAll(filepath.Dir(path), 0700)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func installStatusLine(uninstall bool) error {
	path := claudeSettingsPath()
	settings := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("не смог прочитать %s (%v) — ничего не менял", path, err)
		}
	}
	if uninstall {
		data, err := os.ReadFile(statusLineBackupPath())
		if err != nil {
			delete(settings, "statusLine")
		} else {
			var prev map[string]any
			if json.Unmarshal(data, &prev) == nil && prev["statusLine"] != nil {
				settings["statusLine"] = prev["statusLine"]
			} else {
				delete(settings, "statusLine")
			}
		}
		if err := writeJSONFile(path, settings); err != nil {
			return err
		}
		fmt.Println("вернул прежнюю строку статуса — перезапусти сессии Claude")
		return nil
	}
	if data, err := os.ReadFile(path); err == nil {
		if _, err := os.Stat(path + ".orchestra-backup"); os.IsNotExist(err) {
			_ = os.WriteFile(path+".orchestra-backup", data, 0600) // the whole file, just in case
		}
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if old, ok := settings["statusLine"]; ok {
		if _, err := os.Stat(statusLineBackupPath()); os.IsNotExist(err) {
			_ = writeJSONFile(statusLineBackupPath(), map[string]any{"statusLine": old})
		}
	}
	settings["statusLine"] = map[string]any{"type": "command", "command": self + " statusline", "padding": 0}
	if err := writeJSONFile(path, settings); err != nil {
		return err
	}
	fmt.Printf("готово: под полем ввода Claude — мини-питомец и плотность.\nПрежняя строка сохранена в %s (вернуть: cs statusline --uninstall).\nНовые сессии подхватят сразу, открытые — после перезапуска.\n", statusLineBackupPath())
	return nil
}

var statuslineCmd = &cobra.Command{
	Use:   "statusline",
	Short: "Мини-питомец и плотность для строки статуса Claude Code (--install — поставить)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if statuslineInstall || statuslineUninstall {
			return installStatusLine(statuslineUninstall)
		}
		fmt.Print(statusLineText())
		return nil
	},
}

func init() {
	statuslineCmd.Flags().BoolVar(&statuslineInstall, "install", false, "поставить в ~/.claude/settings.json (прежнюю строку сохранить)")
	statuslineCmd.Flags().BoolVar(&statuslineUninstall, "uninstall", false, "вернуть прежнюю строку статуса")
	statuslineCmd.SilenceUsage = true
	rootCmd.AddCommand(statuslineCmd)
}
