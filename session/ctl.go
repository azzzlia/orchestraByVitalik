package session

import (
	"claude-squad/config"
	"claude-squad/session/git"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// StatusEntry is one session in the status snapshot read by `cs ctl list`.
type StatusEntry struct {
	Number      int     `json:"number"`
	Title       string  `json:"title"`
	Status      string  `json:"status"`
	Stage       string  `json:"stage,omitempty"`
	Recap       string  `json:"recap,omitempty"`
	Note        string  `json:"note,omitempty"`
	Branch      string  `json:"branch,omitempty"`
	Folder      string  `json:"folder,omitempty"`
	Path        string  `json:"path,omitempty"`
	TmuxName    string  `json:"tmux_name,omitempty"`
	NeedsInput  bool    `json:"needs_input,omitempty"`
	Conductor   bool    `json:"conductor,omitempty"`
	Assistant   bool    `json:"assistant,omitempty"`
	ConvID      string  `json:"conversation_id,omitempty"`
	UsageToday  float64 `json:"usage_today,omitempty"`
	IdleMinutes int     `json:"idle_minutes,omitempty"`
	// HasDraft: text sits in the input box; DraftIdleSec — seconds since it changed.
	HasDraft     bool `json:"has_draft,omitempty"`
	DraftIdleSec int  `json:"draft_idle_sec,omitempty"`
	// Menu: a choice menu waits for an answer (cs ctl answer), MenuMinutes — for how long.
	Menu        *ChoiceMenu `json:"menu,omitempty"`
	MenuMinutes int         `json:"menu_minutes,omitempty"`
	// MRs: the session's merge requests and their state (cs ctl mr).
	MRs []MRRef `json:"mrs,omitempty"`
	// Group: main, qa or dev (see GroupOf); Color — the color label.
	Group string `json:"group,omitempty"`
	Color string `json:"color,omitempty"`
}

// StatusSnapshot is what Squad writes to status.json for other tools.
type StatusSnapshot struct {
	UpdatedAt time.Time     `json:"updated_at"`
	UIWaiting string        `json:"ui_waiting,omitempty"`
	Sessions  []StatusEntry `json:"sessions"`
	// Terminal is the terminal app Squad runs in (TERM_PROGRAM), Dir its folder:
	// cs ctl focus uses them to bring Squad to the front or start it.
	Terminal string `json:"terminal,omitempty"`
	Dir      string `json:"dir,omitempty"`
	// Usage of the plan limits in percent (0 when not calibrated) and VPN state.
	Usage5hPct   float64 `json:"usage_5h_pct,omitempty"`
	UsageWeekPct float64 `json:"usage_week_pct,omitempty"`
	VPNDown      bool    `json:"vpn_down,omitempty"`
}

// SnapshotExtra is what Squad adds to the snapshot besides the sessions.
var SnapshotExtra struct {
	Usage5hPct, UsageWeekPct float64
	VPNDown                  bool
}

func configPath(name string) (string, error) {
	dir, err := config.GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// SnapshotEntry describes one instance for the status snapshot.
func (i *Instance) SnapshotEntry() StatusEntry {
	label, _ := i.StatusLabel()
	e := StatusEntry{
		Number: i.Number, Title: i.Title, Status: label, Stage: i.Stage(), Recap: i.Recap,
		Note: i.Note, Branch: i.Branch, Folder: i.FolderLabel(), Path: i.GetWorktreePath(),
		NeedsInput: i.NeedsInput, Conductor: i.Conductor, Assistant: i.Assistant,
		ConvID: i.ClaudeConversationID(), UsageToday: i.UsageToday, MRs: i.MRs, Group: i.GroupOf(), Color: i.Color,
	}
	if i.tmuxSession != nil {
		e.TmuxName = i.tmuxSession.Name()
	}
	if i.Menu != nil {
		e.Menu = i.Menu
		e.MenuMinutes = int(time.Since(i.MenuSince).Minutes())
	}
	if i.Status == Ready && !i.IdleSince.IsZero() {
		e.IdleMinutes = int(time.Since(i.IdleSince).Minutes())
	}
	if i.Draft != "" && !i.DraftSince.IsZero() {
		e.HasDraft, e.DraftIdleSec = true, int(time.Since(i.DraftSince).Seconds())
	}
	return e
}

// WriteStatusSnapshot saves the current state of all sessions to status.json.
func WriteStatusSnapshot(instances []*Instance, uiWaiting string) error {
	path, err := configPath("status.json")
	if err != nil {
		return err
	}
	snap := StatusSnapshot{UpdatedAt: time.Now(), UIWaiting: uiWaiting, Terminal: os.Getenv("TERM_PROGRAM")}
	snap.Dir, _ = os.Getwd()
	snap.Usage5hPct, snap.UsageWeekPct, snap.VPNDown = SnapshotExtra.Usage5hPct, SnapshotExtra.UsageWeekPct, SnapshotExtra.VPNDown
	for _, inst := range instances {
		if inst.Started() {
			snap.Sessions = append(snap.Sessions, inst.SnapshotEntry())
		}
	}
	sort.Slice(snap.Sessions, func(a, b int) bool { return snap.Sessions[a].Number < snap.Sessions[b].Number })
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadStatusSnapshot loads status.json.
func ReadStatusSnapshot() (*StatusSnapshot, error) {
	path, err := configPath("status.json")
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("нет снимка статусов (Squad запущен?): %w", err)
	}
	var snap StatusSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

// SessionRequest asks a running Squad to open a session in an existing folder.
type SessionRequest struct {
	Title     string `json:"title"`
	Dir       string `json:"dir"`
	Branch    string `json:"branch,omitempty"`
	Prompt    string `json:"prompt,omitempty"`
	Program   string `json:"program,omitempty"`
	Color     string `json:"color,omitempty"`
	Note      string `json:"note,omitempty"`
	Conductor bool   `json:"conductor,omitempty"`
	Assistant bool   `json:"assistant,omitempty"`
	// Kill closes the session with this title (folder and branch are kept).
	Kill bool `json:"kill,omitempty"`
	// Wake starts a sleeping/paused session again (same conversation).
	Wake bool `json:"wake,omitempty"`
	// Restart restarts the agent with the same conversation (MCP reconnect).
	Restart bool `json:"restart,omitempty"`
	// Mark says whom the idle session waits for: "user", "wait" or "clear".
	Mark       string `json:"mark,omitempty"`
	MarkReason string `json:"mark_reason,omitempty"`
	// MR records a merge request of the session and its state (cs ctl mr):
	// MRState is "" / open, merged, closed or ignore.
	// Group sets the session's group ("qa", "dev", "auto"); with Dir — of the new one.
	Group    string `json:"group,omitempty"`
	SetGroup bool   `json:"set_group,omitempty"`
	MR       string `json:"mr,omitempty"`
	MRState  string `json:"mr_state,omitempty"`
	// Focus opens the session full screen in Squad (from a notification click).
	Focus bool      `json:"focus,omitempty"`
	At    time.Time `json:"at,omitempty"`
}

func requestsDir() (string, error) {
	return configPath("requests")
}

// SubmitSessionRequest queues a request for the running Squad to pick up.
func SubmitSessionRequest(req SessionRequest) (string, error) {
	if req.At.IsZero() {
		req.At = time.Now()
	}
	dir, err := requestsDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("%d.json", time.Now().UnixNano()))
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return "", err
	}
	return path, os.Rename(tmp, path)
}

// TakeSessionRequests returns queued requests and removes them from the queue.
func TakeSessionRequests() []SessionRequest {
	return TakeSessionRequestsWhere(func(SessionRequest) bool { return true })
}

// requestTTL: a request nobody could act on for this long is dropped, so a
// restart asked for at night does not fire hours later out of the blue.
const requestTTL = 10 * time.Minute

// TakeSessionRequestsWhere takes the queued requests take accepts and leaves the
// rest in the queue. Expired requests are dropped (and reported via ctl result).
func TakeSessionRequestsWhere(take func(SessionRequest) bool) []SessionRequest {
	dir, err := requestsDir()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var reqs []SessionRequest
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var req SessionRequest
		if json.Unmarshal(data, &req) != nil || req.Title == "" || !(req.Dir != "" || req.Kill || req.Wake || req.Restart || req.Focus || req.Mark != "" || req.MR != "" || req.SetGroup) {
			_ = os.Remove(path)
			continue
		}
		if req.At.IsZero() {
			if info, err := e.Info(); err == nil {
				req.At = info.ModTime()
			}
		}
		ttl := requestTTL
		switch {
		case req.Focus:
			ttl = 2 * time.Minute // a click from long ago: don't jump into a session out of the blue
		case !(req.Wake || req.Restart || req.Mark != "" || req.MR != "" || req.SetGroup):
			// new/kill wait (while a window is open) as long as needed: cs ctl new has
			// already told the conductor «запрос принят».
			ttl = 24 * time.Hour
		}
		if time.Since(req.At) > ttl {
			_ = os.Remove(path)
			switch {
			case req.Wake || req.Restart:
				WriteCtlResult(req.Title, "ошибка: запрос просрочен — Squad не смог выполнить его за 10 минут")
			case req.Kill || req.Dir != "":
				WriteCtlResult(req.Title, "ошибка: запрос просрочен — сутки не было возможности его выполнить (в Squad всё время было открыто окно); подай заново")
			}
			continue
		}
		if !take(req) {
			continue
		}
		_ = os.Remove(path)
		reqs = append(reqs, req)
	}
	return reqs
}

// RemoteControlWorkers: start workers with Claude Code's Remote Control (remote_control
// in the config), so their permission prompts reach the Claude app on the phone.
var RemoteControlWorkers bool

// WorkerNoMenuNote is added to the first message of a worker the conductor starts.
const WorkerNoMenuNote = "(Ты воркер под дирижёром: не используй интерактивные меню выбора (AskUserQuestion) — задавай вопросы обычным текстом, варианты перечисляй списком.)"

// NewInstanceInFolder creates an (unstarted) instance that runs in an existing folder,
// which Squad treats as the user's own: never removed, committed to or pushed.
func NewInstanceInFolder(req SessionRequest, defaultProgram string) (*Instance, error) {
	dir, err := filepath.Abs(req.Dir)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("папки %s нет", dir)
	}
	program := req.Program
	if program == "" {
		program = defaultProgram
	}
	prompt := req.Prompt
	if !req.Conductor && !req.Assistant {
		// A worker under the conductor must not stop on a menu only a key press
		// answers: Claude Code gets no AskUserQuestion tool and asks in text.
		if f := strings.Fields(program); len(f) > 0 && filepath.Base(f[0]) == "claude" {
			// Remote Control: the session shows up in the Claude app under its title, and
			// its permission prompts can be answered there by the user from the phone.
			if RemoteControlWorkers && !strings.Contains(program, "--remote-control") && !strings.Contains(program, "--rc") {
				program += " --remote-control '" + strings.ReplaceAll(req.Title, "'", "") + "'"
			}
			// Worker permissions: commit, push of its own branch, new branches.
			EnsureWorkerSettings()
			program += workerSettingsArg(program)
			if !strings.Contains(program, "AskUserQuestion") {
				program += " --disallowedTools AskUserQuestion"
			}
		}
		if prompt != "" {
			prompt += "\n\n" + WorkerNoMenuNote
		}
	}
	branch := req.Branch
	if branch == "" {
		branch = filepath.Base(dir)
	}
	now := time.Now()
	inst := &Instance{
		Title: req.Title, Path: dir, Branch: branch, Status: Ready, Program: program,
		CreatedAt: now, UpdatedAt: now, Color: req.Color, Note: req.Note, Group: req.Group,
		Conductor: req.Conductor, Assistant: req.Assistant, PendingPrompt: prompt, IdleSince: now,
		ByConductor: !req.Conductor && !req.Assistant,
	}
	inst.gitWorktree = git.NewGitWorktreeFromStorage(dir, dir, req.Title, branch, "", true, true)
	inst.externalReady = true
	return inst, nil
}

// LastIssuedNumber returns the highest session number ever handed out.
func LastIssuedNumber() int {
	path, err := configPath("last-number")
	if err != nil {
		return 0
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n := 0
	fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &n)
	return n
}

// SetLastIssuedNumber records the highest session number handed out.
func SetLastIssuedNumber(n int) {
	if path, err := configPath("last-number"); err == nil {
		_ = os.WriteFile(path, []byte(fmt.Sprint(n)), 0600)
	}
}

// WriteCtlResult records the outcome of the last wake/restart for a session, so
// `cs ctl` can report it.
func WriteCtlResult(title, result string) {
	dir, err := configPath("ctl-results")
	if err != nil {
		return
	}
	_ = os.MkdirAll(dir, 0700)
	data, _ := json.Marshal(map[string]string{"title": title, "result": result, "at": time.Now().Format(time.RFC3339Nano)})
	// One file per session: two requests handled in the same tick must not
	// overwrite each other's answer.
	path := filepath.Join(dir, ctlResultName(title))
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0600) == nil {
		_ = os.Rename(tmp, path)
	}
}

func ctlResultName(title string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(title))
	return fmt.Sprintf("%x.json", h.Sum64())
}

// ReadCtlResult returns the result Squad wrote for this session after since.
func ReadCtlResult(title string, since time.Time) (string, bool) {
	dir, err := configPath("ctl-results")
	if err != nil {
		return "", false
	}
	data, err := os.ReadFile(filepath.Join(dir, ctlResultName(title)))
	if err != nil {
		return "", false
	}
	var r map[string]string
	if json.Unmarshal(data, &r) != nil || r["title"] != title {
		return "", false
	}
	at, err := time.Parse(time.RFC3339Nano, r["at"])
	if err != nil || at.Before(since.Add(-time.Second)) {
		return "", false
	}
	return r["result"], true
}
