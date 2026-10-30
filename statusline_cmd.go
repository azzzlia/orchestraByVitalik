package main

import (
	"claude-squad/config"
	"claude-squad/session"
	"claude-squad/ui"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// `cs statusline` prints a one-line pet with the density score for Claude Code's
// status line (under the input box). `--install` puts it into ~/.claude/settings.json.

var (
	statuslineInstall   bool
	statuslineUninstall bool
)

// microPets are the status-line faces, saddest first: one per overview pet mood.
var microPets = []string{"(╥_╥)", "(T_T)", "(;_;)", "(-_-)", "(¬_¬)", "(•_•)", "(•ᴗ•)", "(^ᴗ^)", "(^▽^)", "(≧ᴗ≦)", "(✧ᴗ✧)", "(≧ᴗ≦)✧"}

// osMicro are the status-line faces of «ОС», grumpiest first.
var osMicro = []string{"(ಠ益ಠ)", "(ಠ_ಠ)", "(¬_¬)?", "(-_-)⌚", "(-_-)", "(•_•)", "(•‿•)", "(^‿^)", "(^▽^)", "(^▽^)b", "(⌐■_■)", "(⌐■‿■)♥"}

func microPet(v float64) string {
	m := ui.PetMood(v)
	if n := len(ui.PetMoodLabels); n > 0 && len(microPets) != n {
		m = m * len(microPets) / n
	}
	if m < 0 {
		m = 0
	}
	if m >= len(microPets) {
		m = len(microPets) - 1
	}
	return microPets[m]
}

func fmtScore(v float64) string { return strings.Replace(fmt.Sprintf("%.1f", v), ".", ",", 1) }

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
	t39 := ""
	if tv, tok := session.CachedT39(); tok {
		t39 = fmt.Sprintf(" \x1b[2m· t39\x1b[0m \x1b[%sm%s\x1b[0m", scoreColor(tv), fmtScore(tv))
	}
	if !ok {
		return "\x1b[2m(•ᴗ•) плт —\x1b[0m" + t39
	}
	if cfg.Pet == "off" {
		return fmt.Sprintf("плт \x1b[%sm%s\x1b[0m%s", scoreColor(v), fmtScore(v), t39)
	}
	kind, face := cfg.Pet, microPet(v)
	m := ui.PetMood(v)
	switch kind {
	case "os":
		face = osMicro[m]
	case "dog", "cat":
	default:
		kind = "tyan"
	}
	var own ui.PetPhrases
	if dir, err := config.GetConfigDir(); err == nil {
		own = ui.LoadPetPhrases(filepath.Join(dir, "pet-phrases.json"))
	}
	phrase := ui.PetPhraseFor(kind, m, time.Now(), own)
	return fmt.Sprintf("\x1b[2m%s «%s»\x1b[0m плт \x1b[%sm%s\x1b[0m%s", face, phrase, scoreColor(v), fmtScore(v), t39)
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
