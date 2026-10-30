package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// PlanUsage is how full the plan's limits are, exactly as Claude Code's /usage
// shows them: Squad asks the same Anthropic endpoint with Claude Code's own sign-in.
type PlanUsage struct {
	FiveHour, Week       float64 // percent
	FiveReset, WeekReset time.Time
	At                   time.Time
}

const planUsageURL = "https://api.anthropic.com/api/oauth/usage"

// ErrKeychainDenied: the user pressed "Deny" on the keychain prompt; do not ask again soon.
var ErrKeychainDenied = errors.New("доступ к связке ключей не разрешён — точные лимиты выключены до перезапуска Squad")

// claudeOAuthToken returns Claude Code's sign-in token: from the macOS keychain
// ("Claude Code-credentials"), else from ~/.claude/.credentials.json.
func claudeOAuthToken(ctx context.Context) (string, error) {
	var raw []byte
	if runtime.GOOS == "darwin" {
		out, err := exec.CommandContext(ctx, "security", "find-generic-password", "-s", "Claude Code-credentials", "-w").Output()
		if err == nil {
			raw = out
		} else if ee, ok := err.(*exec.ExitError); ok && (ee.ExitCode() == 128 || ee.ExitCode() == 36) {
			return "", ErrKeychainDenied // 128: "User canceled", 36: locked and not unlocked
		}
	}
	if len(raw) == 0 {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		raw, err = os.ReadFile(filepath.Join(home, ".claude", ".credentials.json"))
		if err != nil {
			return "", errors.New("нет входа Claude Code (связка ключей или ~/.claude/.credentials.json)")
		}
	}
	var c struct {
		OAuth struct {
			AccessToken string `json:"accessToken"`
			ExpiresAt   int64  `json:"expiresAt"` // ms
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(raw))), &c); err != nil || c.OAuth.AccessToken == "" {
		return "", errors.New("вход Claude Code не прочитан")
	}
	if c.OAuth.ExpiresAt > 0 && time.UnixMilli(c.OAuth.ExpiresAt).Before(time.Now()) {
		return "", errors.New("вход Claude Code истёк — он обновится, когда Claude Code сделает запрос")
	}
	return c.OAuth.AccessToken, nil
}

// FetchPlanUsage asks Anthropic how full the limits are. The token is only sent
// to api.anthropic.com and never logged.
func FetchPlanUsage(ctx context.Context) (PlanUsage, error) {
	tok, err := claudeOAuthToken(ctx)
	if err != nil {
		return PlanUsage{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, planUsageURL, nil)
	if err != nil {
		return PlanUsage{}, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return PlanUsage{}, fmt.Errorf("нет связи с Anthropic: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return PlanUsage{}, fmt.Errorf("Anthropic ответил %d", resp.StatusCode)
	}
	return parsePlanUsage(resp.Body)
}

type planWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    string   `json:"resets_at"`
}

func parsePlanUsage(r interface{ Read([]byte) (int, error) }) (PlanUsage, error) {
	var d struct {
		FiveHour *planWindow `json:"five_hour"`
		SevenDay *planWindow `json:"seven_day"`
	}
	if err := json.NewDecoder(r).Decode(&d); err != nil {
		return PlanUsage{}, fmt.Errorf("непонятный ответ: %v", err)
	}
	if d.FiveHour == nil && d.SevenDay == nil {
		return PlanUsage{}, errors.New("в ответе нет лимитов")
	}
	u := PlanUsage{At: time.Now()}
	if w := d.FiveHour; w != nil && w.Utilization != nil {
		u.FiveHour = *w.Utilization
		u.FiveReset, _ = time.Parse(time.RFC3339, w.ResetsAt)
	}
	if w := d.SevenDay; w != nil && w.Utilization != nil {
		u.Week = *w.Utilization
		u.WeekReset, _ = time.Parse(time.RFC3339, w.ResetsAt)
	}
	return u, nil
}
