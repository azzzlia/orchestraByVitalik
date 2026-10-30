package config

import (
	"claude-squad/log"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	ConfigFileName = "config.json"
	defaultProgram = "claude"
)

// GetConfigDir returns the path to the application's configuration directory
func GetConfigDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get config home directory: %w", err)
	}
	return filepath.Join(homeDir, ".claude-squad"), nil
}

// Profile represents a named program configuration
type Profile struct {
	Name    string `json:"name"`
	Program string `json:"program"`
}

// Config represents the application configuration
type Config struct {
	// DefaultProgram is the default program to run in new instances
	DefaultProgram string `json:"default_program"`
	// AutoYes is a flag to automatically accept all prompts.
	AutoYes bool `json:"auto_yes"`
	// DaemonPollInterval is the interval (ms) at which the daemon polls sessions for autoyes mode.
	DaemonPollInterval int `json:"daemon_poll_interval"`
	// BranchPrefix is the prefix used for git branches created by the application.
	BranchPrefix string `json:"branch_prefix"`
	// Profiles is a list of named program profiles.
	Profiles []Profile `json:"profiles,omitempty"`
	// AutoPauseMinutes puts a session to sleep (stops the agent to free memory) after
	// it has waited for the user this long. 0 uses the default, negative disables.
	AutoPauseMinutes int `json:"auto_pause_minutes,omitempty"`
	// ConductorIntervalMinutes is how often Squad tells the conductor what changed
	// (only when something did). 0 uses the default, negative disables.
	ConductorIntervalMinutes int `json:"conductor_interval_minutes,omitempty"`
	// MenuAutoMinutes: a worker's choice menu (AskUserQuestion) the conductor has not
	// answered in this many minutes gets its "(Recommended)" option picked by Squad,
	// unless the menu is about push/merge/deletion and the like — those go to you.
	// Default 10; -1 — never pick, only tell you.
	MenuAutoMinutes int `json:"menu_auto_minutes,omitempty"`
	// DailyJobs open a session with a prompt once a day inside a time window.
	DailyJobs []DailyJob `json:"daily_jobs,omitempty"`
	// VPNCheck lists host:port addresses reachable only through the work VPN
	// (e.g. "git.example.com:443"). Squad probes them to see when the VPN drops and comes
	// back, and then gets the sessions that got stuck on it going again.
	VPNCheck []string `json:"vpn_check,omitempty"`
	// MCPToolTimeoutSeconds limits one MCP tool call in sessions Squad starts, so a
	// call whose connection died with the VPN fails instead of hanging forever.
	// 0 uses the default, negative leaves Claude Code's own limit.
	MCPToolTimeoutSeconds int `json:"mcp_tool_timeout_seconds,omitempty"`
	// TokenBudget5h / TokenBudgetWeek are the plan limits in Squad's weighted
	// tokens, calibrated with `cs usage --calibrate 5h=NN week=NN` (NN — what
	// Claude's /usage shows). 0 — unknown: Squad shows amounts without percentages.
	TokenBudget5h   float64 `json:"token_budget_5h,omitempty"`
	TokenBudgetWeek float64 `json:"token_budget_week,omitempty"`
	// UsageWarnPercent: notify when a limit is this full (default 70).
	// UsageThrottlePercent: from here the conductor is woken 3x less often (default 85).
	// PlanUsageOff: do not ask Anthropic for the real limits (the numbers of /usage,
	// with Claude Code's sign-in from the keychain); then only the estimate above.
	PlanUsageOff         bool `json:"plan_usage_off,omitempty"`
	UsageWarnPercent     int  `json:"usage_warn_percent,omitempty"`
	UsageThrottlePercent int  `json:"usage_throttle_percent,omitempty"`
	// MaxActiveSessions is how many worker sessions can run at once without
	// slowing the Mac or burning the limits (default 6). Used for "есть ресурс".
	MaxActiveSessions int `json:"max_active_sessions,omitempty"`
	// CapacityNotify: tell the user (no tokens) when there is room to take a task
	// from the team board, on workdays 10–19, at most every 2 hours. Default on.
	CapacityNotifyOff bool `json:"capacity_notify_off,omitempty"`
	// Pet in the corner of the overview (v): "" or "tyan" — the built-in one, "os" —
	// the manager, "dog" — a puppy, "cat" — a kitten (p in the overview cycles them),
	// "custom" — your own text art from PetDir (1.txt … 7.txt, frames split by
	// a "---" line, saddest first), "off" — none.
	Pet    string `json:"pet,omitempty"`
	PetDir string `json:"pet_dir,omitempty"`
	// RemoteControl: workers Squad starts (cs ctl new, daily jobs) get Claude Code's
	// Remote Control named after the session: in the Claude app on the phone you see
	// them and answer their permission prompts yourself (push notifications: /config →
	// «Push when actions required»).
	RemoteControl bool `json:"remote_control,omitempty"`
	// TrustFolders: when a session opens in a folder Claude Code has not seen yet, Squad
	// answers «Yes, I trust this folder» itself (the conductor opens new folders at night).
	// Off by default: the session shows «ждёт тебя · подтверди доверие к папке».
	TrustFolders bool `json:"trust_folders,omitempty"`
	// MorningBriefAt: when the morning brief goes out ("09:00" when empty, "off" — never):
	// what the night brought and who waits for the user, as a notification on the Mac
	// and, when BriefChat is set, through the conductor to that chat (reaches the phone).
	MorningBriefAt string `json:"morning_brief_at,omitempty"`
	// BriefChat: the chat the conductor sends the morning brief to ("Saved Messages"); empty —
	// the brief is only a notification on the Mac.
	BriefChat string `json:"brief_chat,omitempty"`
	// TeamBoard: the team's task board ("#team") the "есть ресурс" notification points
	// to (the assistant's profile knows how to read it); empty — a generic notification.
	TeamBoard string `json:"team_board,omitempty"`
	// WebUI: the web interface (sessions, «ждут тебя», live terminals). "off" turns it
	// off; on by default at 127.0.0.1 and the Mac's Tailscale address, behind a key.
	WebUI   string `json:"web_ui,omitempty"`
	WebPort int    `json:"web_port,omitempty"`
	// PetStill: the pet stands in the corner instead of walking along the bottom.
	PetStill bool `json:"pet_still,omitempty"`
	// PetBig: the old big portrait pet instead of the small full-height one.
	PetBig bool `json:"pet_big,omitempty"`
	// DensityCommand prints your "плотность" score; Squad runs it every 10 minutes.
	// Without it, set the score by hand: cs density 10,4.
	DensityCommand string `json:"density_command,omitempty"`
	// T39Command prints the official score from t39 (shown next to the local one).
	// Empty: the status-line command that was there before cs statusline --install.
	T39Command string `json:"t39_command,omitempty"`
}

// MaxActive returns MaxActiveSessions or its default.
func (c *Config) MaxActive() int {
	if c.MaxActiveSessions > 0 {
		return c.MaxActiveSessions
	}
	return 6
}

// UsageThresholds returns the warn and throttle percentages.
func (c *Config) UsageThresholds() (warn, throttle float64) {
	warn, throttle = 70, 85
	if c.UsageWarnPercent > 0 {
		warn = float64(c.UsageWarnPercent)
	}
	if c.UsageThrottlePercent > 0 {
		throttle = float64(c.UsageThrottlePercent)
	}
	return warn, throttle
}

// DefaultMCPToolTimeoutSeconds is used when MCPToolTimeoutSeconds is not set.
const DefaultMCPToolTimeoutSeconds = 600

// MCPToolTimeout returns the MCP tool call limit in seconds, or 0 for none.
func (c *Config) MCPToolTimeout() int {
	switch {
	case c.MCPToolTimeoutSeconds < 0:
		return 0
	case c.MCPToolTimeoutSeconds == 0:
		return DefaultMCPToolTimeoutSeconds
	default:
		return c.MCPToolTimeoutSeconds
	}
}

// DailyJob is a session Squad opens once a day between From and To (HH:MM, local).
type DailyJob struct {
	Name   string `json:"name"`
	From   string `json:"from"`
	To     string `json:"to"`
	Dir    string `json:"dir"`
	Title  string `json:"title"`
	Prompt string `json:"prompt"`
}

// DueToday reports whether now falls inside the job's window.
func (j DailyJob) DueToday(now time.Time) bool {
	parse := func(s string) (int, bool) {
		var h, m int
		if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil {
			return 0, false
		}
		return h*60 + m, true
	}
	from, ok1 := parse(j.From)
	to, ok2 := parse(j.To)
	cur := now.Hour()*60 + now.Minute()
	return ok1 && ok2 && cur >= from && cur < to
}

// ConductorInterval returns how often the conductor gets a digest, or 0 if disabled.
func (c *Config) ConductorInterval() time.Duration {
	switch {
	case c.ConductorIntervalMinutes < 0:
		return 0
	case c.ConductorIntervalMinutes == 0:
		return 12 * time.Minute
	default:
		return time.Duration(c.ConductorIntervalMinutes) * time.Minute
	}
}

// MenuAuto returns after how long a worker's menu is answered for you (0 — never).
func (c *Config) MenuAuto() time.Duration {
	switch {
	case c.MenuAutoMinutes < 0:
		return 0
	case c.MenuAutoMinutes == 0:
		return 10 * time.Minute
	default:
		return time.Duration(c.MenuAutoMinutes) * time.Minute
	}
}

// DefaultAutoPauseMinutes is used when AutoPauseMinutes is not set.
const DefaultAutoPauseMinutes = 120

// AutoPauseAfter returns the idle time after which sessions are put to sleep, or 0.
func (c *Config) AutoPauseAfter() time.Duration {
	switch {
	case c.AutoPauseMinutes < 0:
		return 0
	case c.AutoPauseMinutes == 0:
		return DefaultAutoPauseMinutes * time.Minute
	default:
		return time.Duration(c.AutoPauseMinutes) * time.Minute
	}
}

// GetProgram returns the program to run. If Profiles is non-empty and
// DefaultProgram matches a profile name, that profile's Program is returned.
// Otherwise DefaultProgram is returned as-is.
func (c *Config) GetProgram() string {
	for _, p := range c.Profiles {
		if p.Name == c.DefaultProgram {
			return p.Program
		}
	}
	return c.DefaultProgram
}

// GetProfiles returns a unified list of profiles. If Profiles is defined,
// those are returned with the default profile first. Otherwise, a single
// profile is synthesized from DefaultProgram.
func (c *Config) GetProfiles() []Profile {
	if len(c.Profiles) == 0 {
		return []Profile{{Name: c.DefaultProgram, Program: c.DefaultProgram}}
	}
	// Reorder so the default profile comes first.
	profiles := make([]Profile, 0, len(c.Profiles))
	for _, p := range c.Profiles {
		if p.Name == c.DefaultProgram {
			profiles = append(profiles, p)
			break
		}
	}
	for _, p := range c.Profiles {
		if p.Name != c.DefaultProgram {
			profiles = append(profiles, p)
		}
	}
	return profiles
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	program, err := GetClaudeCommand()
	if err != nil {
		log.ErrorLog.Printf("failed to get claude command: %v", err)
		program = defaultProgram
	}

	return &Config{
		DefaultProgram:     program,
		AutoYes:            false,
		DaemonPollInterval: 1000,
		BranchPrefix: func() string {
			user, err := user.Current()
			if err != nil || user == nil || user.Username == "" {
				log.ErrorLog.Printf("failed to get current user: %v", err)
				return "session/"
			}
			return fmt.Sprintf("%s/", strings.ToLower(user.Username))
		}(),
	}
}

// GetClaudeCommand attempts to find the "claude" command in the user's shell
// It checks in the following order:
// 1. Shell alias resolution: using "which" command
// 2. PATH lookup
//
// If both fail, it returns an error.
func GetClaudeCommand() (string, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash" // Default to bash if SHELL is not set
	}

	// Force the shell to load the user's profile and then run the command
	// For zsh, source .zshrc; for bash, source .bashrc
	var shellCmd string
	if strings.Contains(shell, "zsh") {
		shellCmd = "source ~/.zshrc &>/dev/null || true; which claude"
	} else if strings.Contains(shell, "bash") {
		shellCmd = "source ~/.bashrc &>/dev/null || true; which claude"
	} else {
		shellCmd = "which claude"
	}

	cmd := exec.Command(shell, "-c", shellCmd)
	output, err := cmd.Output()
	if err == nil && len(output) > 0 {
		path := strings.TrimSpace(string(output))
		if path != "" {
			// Check if the output is an alias definition and extract the actual path
			// Handle formats like "claude: aliased to /path/to/claude" or other shell-specific formats
			aliasRegex := regexp.MustCompile(`(?:aliased to|->|=)\s*([^\s]+)`)
			matches := aliasRegex.FindStringSubmatch(path)
			if len(matches) > 1 {
				path = matches[1]
			}
			return path, nil
		}
	}

	// Otherwise, try to find in PATH directly
	claudePath, err := exec.LookPath("claude")
	if err == nil {
		return claudePath, nil
	}

	return "", fmt.Errorf("claude command not found in aliases or PATH")
}

func LoadConfig() *Config {
	configDir, err := GetConfigDir()
	if err != nil {
		log.ErrorLog.Printf("failed to get config directory: %v", err)
		return DefaultConfig()
	}

	configPath := filepath.Join(configDir, ConfigFileName)
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Create and save default config if file doesn't exist
			defaultCfg := DefaultConfig()
			if saveErr := saveConfig(defaultCfg); saveErr != nil {
				log.WarningLog.Printf("failed to save default config: %v", saveErr)
			}
			return defaultCfg
		}

		log.WarningLog.Printf("failed to get config file: %v", err)
		return DefaultConfig()
	}

	// Fields the file does not set keep their defaults (install-mac.sh may write only a
	// profile's few keys before Squad's first start).
	config := DefaultConfig()
	if err := json.Unmarshal(data, config); err != nil {
		log.ErrorLog.Printf("failed to parse config file: %v", err)
		return DefaultConfig()
	}

	return config
}

// saveConfig saves the configuration to disk
func saveConfig(config *Config) error {
	configDir, err := GetConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	configPath := filepath.Join(configDir, ConfigFileName)
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	return os.WriteFile(configPath, data, 0644)
}

// SaveConfig exports the saveConfig function for use by other packages
func SaveConfig(config *Config) error {
	return saveConfig(config)
}

// MorningBriefTime is the hour and minute of the morning brief; ok=false when off.
func (c *Config) MorningBriefTime() (h, m int, ok bool) {
	at := c.MorningBriefAt
	if at == "" {
		at = "09:00"
	}
	if at == "off" {
		return 0, 0, false
	}
	if _, err := fmt.Sscanf(at, "%d:%d", &h, &m); err != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 9, 0, true
	}
	return h, m, true
}

// WebPortOrDefault is the web interface port (8787 by default).
func (c *Config) WebPortOrDefault() int {
	if c.WebPort > 0 && c.WebPort < 65536 {
		return c.WebPort
	}
	return 8787
}
