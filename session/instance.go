package session

import (
	"claude-squad/config"
	"claude-squad/log"
	"claude-squad/session/git"
	"claude-squad/session/tmux"
	"errors"
	"path/filepath"

	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/atotto/clipboard"
)

type Status int

const (
	// Running is the status when the instance is running and claude is working.
	Running Status = iota
	// Ready is if the claude instance is ready to be interacted with (waiting for user input).
	Ready
	// Loading is if the instance is loading (if we are starting it up or something).
	Loading
	// Paused is if the instance is paused (worktree removed but branch preserved).
	Paused
)

// Instance is a running instance of claude code.
type Instance struct {
	// Color is an optional color label shown next to the title (see SessionColors).
	Color string
	// Note is an optional one-line description of what the session is about.
	Note string
	// Group: "qa" or "dev" set by hand ("" — guessed, see GroupOf).
	Group string
	// Recap is the agent's latest on-screen recap (where the task stands).
	Recap string
	// NeedsInput is true while the agent shows a permission/approval prompt.
	NeedsInput bool
	// PermissionCmd is what the permission prompt asks for ("git push -u origin x").
	PermissionCmd string
	// NeedsSince: when the current permission prompt appeared; PermNotified — the
	// prompt Squad already told the user about.
	NeedsSince   time.Time
	PermNotified string
	// TailLines holds the last lines of the session screen (runtime only).
	TailLines []string
	// Number is the session's stable number shown in the list and notifications.
	Number int
	// Conductor marks the session that orchestrates all others (pinned on top).
	Conductor bool
	// Assistant marks the session where the user asks questions (pinned under the
	// conductor; never written to by the conductor, no digests).
	Assistant bool
	// PendingPrompt is sent once the agent's input box is ready (saved with the
	// session, so a Squad restart does not lose it).
	PendingPrompt string
	// ByConductor: opened by cs ctl new (conductor / daily job). Only such sessions
	// get their menus answered automatically; the user's own sessions never do.
	ByConductor bool
	// externalReady: gitWorktree already points at an existing folder (runtime only).
	externalReady bool
	// inputVisible: the agent's input box is on screen (runtime only).
	inputVisible bool
	// Hibernated is true when the agent process was stopped to free memory; Resume
	// restarts the same conversation.
	Hibernated bool
	// RecapStale is true once the agent moved on from its last recap (runtime only).
	RecapStale bool
	// NetworkIssue is true when the screen shows network/VPN errors (runtime only).
	NetworkIssue bool
	// Interrupted is true when the agent was stopped mid-task (Esc) (runtime only).
	Interrupted bool
	// turnSig, turnNetIssue, turnMCPIssue describe the current turn (runtime only).
	turnSig      string
	turnNetIssue bool
	turnMCPIssue bool
	turnMCPCall  bool
	// WaitReason is what the agent waits for by itself (runtime only), see waitReason.
	WaitReason string
	// Menu is a choice menu (AskUserQuestion) waiting for a key press; MenuSince —
	// when it appeared; MenuEscalated — the conductor did not answer it in time.
	// CallsUser: the conductor or the assistant ended its answer with «⚠ НУЖЕН ТЫ: …»
	// (the reason after it). CallsUserSeen — the call the user already opened.
	CallsUser     string
	CallsUserSeen string
	// TrustPrompt: Claude Code asks whether to trust the folder (a new folder).
	TrustPrompt bool
	// PendingTries: failed attempts to type PendingPrompt (gives up after a few).
	PendingTries  int
	Menu          *ChoiceMenu
	MenuSince     time.Time
	MenuEscalated bool
	MenuHandled   string // key of the menu Squad already acted on (auto-picked or escalated)
	// UsageToday is the session's weighted token usage since midnight (runtime only).
	UsageToday float64
	// LimitHit / LimitReset: the turn ended on a usage limit error, and when it
	// resets if Claude Code said so (runtime only).
	LimitHit   bool
	LimitReset time.Time
	// resumedAt is when the agent was last (re)started with --resume (runtime only).
	resumedAt time.Time
	// MRs: merge requests of the session (seen on its screen or set by cs ctl mr).
	MRs []MRRef
	// IdleSince is when the agent last stopped working (runtime only).
	IdleSince time.Time
	// ActiveAt is when its screen last changed (the agent worked or printed), saved
	// across restarts: the list shows it as the session's last activity.
	ActiveAt time.Time
	// LastDiffAt is when diff stats were last computed (runtime only, monitor goroutine).
	LastDiffAt time.Time
	// Suggestion is Claude Code's greyed-out prompt suggestion, if shown (runtime only).
	Suggestion string
	// Draft is unsent text already typed into the agent's input box (runtime only).
	Draft string
	// DraftSince is when Draft last changed (runtime only): an old draft is forgotten.
	DraftSince time.Time
	// MarkedFor is who the idle session waits for, set by the conductor with
	// cs ctl mark: "user" or "wait" (something outside: review, CI). Cleared when
	// the agent starts working again (runtime only). MarkReason says why.
	MarkedFor  string
	MarkReason string
	// WipedText/WipedAt: the last forgotten draft erased from the box (runtime only),
	// shown on the session row for a while.
	WipedText string
	WipedAt   time.Time

	// Title is the title of the instance.
	Title string
	// Path is the path to the workspace.
	Path string
	// Branch is the branch of the instance.
	Branch string
	// Status is the status of the instance.
	Status Status
	// Program is the program to run in the instance.
	Program string
	// Height is the height of the instance.
	Height int
	// Width is the width of the instance.
	Width int
	// CreatedAt is the time the instance was created.
	CreatedAt time.Time
	// UpdatedAt is the time the instance was last updated.
	UpdatedAt time.Time
	// AutoYes is true if the instance should automatically press enter when prompted.
	AutoYes bool
	// Prompt is the initial prompt to pass to the instance on startup
	Prompt string

	// DiffStats stores the current git diff statistics
	diffStats *git.DiffStats

	// selectedBranch is the existing branch to start on (empty = new branch from HEAD)
	selectedBranch string

	// The below fields are initialized upon calling Start().

	started bool
	// tmuxSession is the tmux session for the instance.
	tmuxSession *tmux.TmuxSession
	// gitWorktree is the git worktree for the instance.
	gitWorktree *git.GitWorktree
}

// ToInstanceData converts an Instance to its serializable form
// SessionColors is the palette cycled with the color key. Empty means no label.
var SessionColors = []string{"", "red", "orange", "yellow", "green", "cyan", "blue", "purple", "pink"}

// CycleColor switches the session to the next color label.
func (i *Instance) CycleColor() {
	next := 0
	for idx, c := range SessionColors {
		if c == i.Color {
			next = (idx + 1) % len(SessionColors)
			break
		}
	}
	i.Color = SessionColors[next]
}

func (i *Instance) ToInstanceData() InstanceData {
	data := InstanceData{
		Color:         i.Color,
		Note:          i.Note,
		Group:         i.Group,
		Recap:         i.Recap,
		Number:        i.Number,
		Conductor:     i.Conductor,
		Assistant:     i.Assistant,
		Hibernated:    i.Hibernated,
		ByConductor:   i.ByConductor,
		PendingPrompt: i.PendingPrompt,
		MRs:           i.MRs,
		ActiveAt:      i.ActiveAt,
		Title:         i.Title,
		Path:          i.Path,
		Branch:        i.Branch,
		Status:        i.Status,
		Height:        i.Height,
		Width:         i.Width,
		CreatedAt:     i.CreatedAt,
		UpdatedAt:     time.Now(),
		Program:       i.Program,
		AutoYes:       i.AutoYes,
	}

	// Only include worktree data if gitWorktree is initialized
	if i.gitWorktree != nil {
		data.Worktree = GitWorktreeData{
			RepoPath:         i.gitWorktree.GetRepoPath(),
			WorktreePath:     i.gitWorktree.GetWorktreePath(),
			SessionName:      i.Title,
			BranchName:       i.gitWorktree.GetBranchName(),
			BaseCommitSHA:    i.gitWorktree.GetBaseCommitSHA(),
			IsExistingBranch: i.gitWorktree.IsExistingBranch(),
			IsExternal:       i.gitWorktree.IsExternal(),
		}
	}

	// Only include diff stats if they exist
	if i.diffStats != nil {
		data.DiffStats = DiffStatsData{
			Added:   i.diffStats.Added,
			Removed: i.diffStats.Removed,
			Content: i.diffStats.Content,
		}
	}

	return data
}

// FromInstanceData creates a new Instance from serialized data
func FromInstanceData(data InstanceData) (*Instance, error) {
	instance := &Instance{
		Title:      data.Title,
		Path:       data.Path,
		Branch:     data.Branch,
		Status:     data.Status,
		Height:     data.Height,
		Width:      data.Width,
		CreatedAt:  data.CreatedAt,
		UpdatedAt:  data.UpdatedAt,
		Program:    data.Program,
		Color:      data.Color,
		Note:       data.Note,
		Group:      data.Group,
		Recap:      data.Recap,
		Number:     data.Number,
		Conductor:  data.Conductor,
		Assistant:  data.Assistant,
		Hibernated: data.Hibernated,
		// Sessions saved before 1.14 have no by_conductor; a worker cs ctl new opened
		// is recognised by the flag Squad added to its program.
		ByConductor:   data.ByConductor || (!data.Conductor && !data.Assistant && strings.Contains(data.Program, "--disallowedTools AskUserQuestion")),
		PendingPrompt: data.PendingPrompt,
		MRs:           data.MRs,
		IdleSince:     time.Now(),
		ActiveAt:      data.ActiveAt,
		gitWorktree: git.NewGitWorktreeFromStorage(
			data.Worktree.RepoPath,
			data.Worktree.WorktreePath,
			data.Worktree.SessionName,
			data.Worktree.BranchName,
			data.Worktree.BaseCommitSHA,
			data.Worktree.IsExistingBranch,
			data.Worktree.IsExternal,
		),
		diffStats: &git.DiffStats{
			Added:   data.DiffStats.Added,
			Removed: data.DiffStats.Removed,
			Content: data.DiffStats.Content,
		},
	}

	if instance.gitWorktree.IsExternal() {
		if err := instance.gitWorktree.EnsureBaseCommit(); err != nil {
			log.WarningLog.Printf("could not record base commit for %q: %v", instance.Title, err)
		}
	}

	if instance.Paused() {
		instance.started = true
		instance.tmuxSession = tmux.NewTmuxSession(instance.Title, instance.Program)
	} else {
		if err := instance.Start(false); err != nil {
			return nil, err
		}
	}

	return instance, nil
}

// Options for creating a new instance
type InstanceOptions struct {
	// Title is the title of the instance.
	Title string
	// Path is the path to the workspace.
	Path string
	// Program is the program to run in the instance (e.g. "claude", "aider --model ollama_chat/gemma3:1b")
	Program string
	// If AutoYes is true, then
	AutoYes bool
	// Branch is an existing branch name to start the session on (empty = new branch from HEAD)
	Branch string
}

func NewInstance(opts InstanceOptions) (*Instance, error) {
	t := time.Now()

	// Convert path to absolute
	absPath, err := filepath.Abs(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	return &Instance{
		Title:          opts.Title,
		Status:         Ready,
		Path:           absPath,
		Program:        opts.Program,
		Height:         0,
		Width:          0,
		CreatedAt:      t,
		UpdatedAt:      t,
		AutoYes:        false,
		selectedBranch: opts.Branch,
	}, nil
}

func (i *Instance) RepoName() (string, error) {
	if !i.started {
		return "", fmt.Errorf("cannot get repo name for instance that has not been started")
	}
	return i.gitWorktree.GetRepoName(), nil
}

func (i *Instance) SetStatus(status Status) {
	i.Status = status
}

// SetSelectedBranch sets the branch to use when starting the instance.
func (i *Instance) SetSelectedBranch(branch string) {
	i.selectedBranch = branch
}

// firstTimeSetup is true if this is a new instance. Otherwise, it's one loaded from storage.
func (i *Instance) Start(firstTimeSetup bool) error {
	if i.Title == "" {
		return fmt.Errorf("instance title cannot be empty")
	}

	var tmuxSession *tmux.TmuxSession
	if i.tmuxSession != nil {
		// Use existing tmux session (useful for testing)
		tmuxSession = i.tmuxSession
	} else {
		// Create new tmux session
		tmuxSession = tmux.NewTmuxSession(i.Title, i.Program)
	}
	i.tmuxSession = tmuxSession

	if firstTimeSetup && !i.externalReady {
		if i.selectedBranch != "" {
			gitWorktree, err := git.NewGitWorktreeFromBranch(i.Path, i.selectedBranch, i.Title)
			if err != nil {
				return fmt.Errorf("failed to create git worktree from branch: %w", err)
			}
			i.gitWorktree = gitWorktree
			i.Branch = i.selectedBranch
		} else {
			gitWorktree, branchName, err := git.NewGitWorktree(i.Path, i.Title)
			if err != nil {
				return fmt.Errorf("failed to create git worktree: %w", err)
			}
			i.gitWorktree = gitWorktree
			i.Branch = branchName
		}
	}

	// Setup error handler to cleanup resources on any error
	var setupErr error
	defer func() {
		if setupErr != nil {
			if cleanupErr := i.Kill(); cleanupErr != nil {
				setupErr = fmt.Errorf("%v (cleanup error: %v)", setupErr, cleanupErr)
			}
		} else {
			i.started = true
		}
	}()

	if !firstTimeSetup {
		// Reuse existing session. If the tmux server died since we last ran (reboot,
		// crash, `tmux kill-server`), the session is gone but the worktree and branch
		// are still on disk. Park the instance as Paused so Resume can rebuild it.
		// Reporting an error here would be worse than useless: LoadInstances aborts on
		// the first failure, so a single dead session would hide every other instance.
		tmuxSession.AdoptLegacySession(i.Title)
		if err := tmuxSession.Restore(); err != nil {
			if errors.Is(err, tmux.ErrSessionNotFound) {
				log.WarningLog.Printf(
					"tmux session for %q no longer exists; pausing instance so it can be resumed", i.Title)
				i.SetStatus(Paused)
				return nil
			}
			setupErr = fmt.Errorf("failed to restore existing session: %w", err)
			return setupErr
		}
	} else {
		// Setup git worktree first
		if err := i.gitWorktree.Setup(); err != nil {
			setupErr = fmt.Errorf("failed to setup git worktree: %w", err)
			return setupErr
		}

		// Create new session
		if err := i.tmuxSession.Start(i.gitWorktree.GetWorktreePath()); err != nil {
			// Cleanup git worktree if tmux session creation fails
			if cleanupErr := i.gitWorktree.Cleanup(); cleanupErr != nil {
				err = fmt.Errorf("%v (cleanup error: %v)", err, cleanupErr)
			}
			setupErr = fmt.Errorf("failed to start new session: %w", err)
			return setupErr
		}
	}

	i.SetStatus(Running)

	return nil
}

// Kill terminates the instance and cleans up all resources
func (i *Instance) Kill() error {
	if !i.started {
		// If instance was never started, just return success
		return nil
	}

	var errs []error

	// Always try to cleanup both resources, even if one fails
	// Clean up tmux session first since it's using the git worktree
	if i.tmuxSession != nil {
		if err := i.tmuxSession.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close tmux session: %w", err))
		}
	}

	// Then clean up git worktree
	if i.gitWorktree != nil {
		if err := i.gitWorktree.Cleanup(); err != nil {
			errs = append(errs, fmt.Errorf("failed to cleanup git worktree: %w", err))
		}
	}

	return i.combineErrors(errs)
}

// combineErrors combines multiple errors into a single error
func (i *Instance) combineErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}

	errMsg := "multiple cleanup errors occurred:"
	for _, err := range errs {
		errMsg += "\n  - " + err.Error()
	}
	return fmt.Errorf("%s", errMsg)
}

func (i *Instance) Preview() (string, error) {
	if !i.started || i.Status == Paused {
		return "", nil
	}
	return i.tmuxSession.CapturePaneContent()
}

func (i *Instance) HasUpdated() (updated bool, hasPrompt bool) {
	if !i.started {
		return false, false
	}
	return i.tmuxSession.HasUpdated()
}

// HasUpdatedWithContent is HasUpdated that also returns the captured screen.
func (i *Instance) HasUpdatedWithContent() (updated bool, hasPrompt bool, content string) {
	if !i.started {
		return false, false, ""
	}
	return i.tmuxSession.HasUpdatedWithContent()
}

// CheckAndHandleTrustPrompt checks for and dismisses the trust prompt for supported programs.
func (i *Instance) CheckAndHandleTrustPrompt() bool {
	if !i.started || i.tmuxSession == nil {
		return false
	}
	switch tmux.ProgramBase(i.Program) {
	case tmux.ProgramClaude, tmux.ProgramAider, tmux.ProgramGemini:
	default:
		return false
	}
	return i.tmuxSession.CheckAndHandleTrustPrompt()
}

// TapEnter sends an enter key press to the tmux session if AutoYes is enabled.
func (i *Instance) TapEnter() {
	if !i.started || !i.AutoYes {
		return
	}
	if err := i.tmuxSession.TapEnter(); err != nil {
		log.ErrorLog.Printf("error tapping enter: %v", err)
	}
}

func (i *Instance) Attach() (chan struct{}, error) {
	if !i.started {
		return nil, fmt.Errorf("cannot attach instance that has not been started")
	}
	return i.tmuxSession.Attach()
}

// ForceDetach takes the user out of this session's full-screen view (shutdown only).
func (i *Instance) ForceDetach() {
	if i.started && i.tmuxSession != nil {
		i.tmuxSession.ForceDetach()
	}
}

func (i *Instance) SetPreviewSize(width, height int) error {
	if !i.started || i.Status == Paused {
		return fmt.Errorf("cannot set preview size for instance that has not been started or " +
			"is paused")
	}
	return i.tmuxSession.SetDetachedSize(width, height)
}

// GetGitWorktree returns the git worktree for the instance
func (i *Instance) GetGitWorktree() (*git.GitWorktree, error) {
	if !i.started {
		return nil, fmt.Errorf("cannot get git worktree for instance that has not been started")
	}
	return i.gitWorktree, nil
}

// GetWorktreePath returns the worktree path for the instance, or empty string if unavailable
// FolderLabel is a short name of the folder the session works in.
func (i *Instance) FolderLabel() string {
	if i.gitWorktree == nil {
		return ""
	}
	if !i.gitWorktree.IsExternal() {
		return "отдельный worktree Squad"
	}
	name := filepath.Base(i.gitWorktree.GetWorktreePath())
	return strings.TrimSuffix(name, ".bx.lan")
}

func (i *Instance) GetWorktreePath() string {
	if i.gitWorktree == nil {
		return ""
	}
	return i.gitWorktree.GetWorktreePath()
}

func (i *Instance) Started() bool {
	return i.started
}

// SetTitle sets the title of the instance. Returns an error if the instance has started.
// We cant change the title once it's been used for a tmux session etc.
func (i *Instance) SetTitle(title string) error {
	if i.started {
		return fmt.Errorf("cannot change title of a started instance")
	}
	i.Title = title
	return nil
}

func (i *Instance) Paused() bool {
	return i.Status == Paused
}

// TmuxAlive returns true if the tmux session is alive. This is a sanity check before attaching.
func (i *Instance) TmuxAlive() bool {
	return i.tmuxSession.DoesSessionExist()
}

// Pause stops the tmux session and removes the worktree, preserving the branch
func (i *Instance) Pause() error {
	if !i.started {
		return fmt.Errorf("cannot pause instance that has not been started")
	}
	if i.Status == Paused {
		return fmt.Errorf("instance is already paused")
	}

	var errs []error

	// External worktree: no auto-commit, no removal. Stop the agent only when its
	// conversation can be resumed exactly; otherwise just detach.
	if i.gitWorktree.IsExternal() {
		if i.ClaudeConversationID() != "" && i.runsClaude() {
			if err := i.tmuxSession.Close(); err != nil {
				log.ErrorLog.Print(err)
			}
			i.Hibernated = true
		} else if err := i.tmuxSession.DetachSafely(); err != nil {
			log.ErrorLog.Print(err)
			return fmt.Errorf("failed to detach tmux session: %w", err)
		}
		i.SetStatus(Paused)
		return nil
	}

	// If the worktree is orphaned (path or .git missing), git cannot operate
	// on it. Skip dirty check and Remove, prune any lingering metadata, then
	// transition to Paused so the user can recover via Resume.
	if valid, err := i.gitWorktree.IsValidWorktree(); err != nil {
		errs = append(errs, fmt.Errorf("failed to validate worktree: %w", err))
		log.ErrorLog.Print(err)
	} else if !valid {
		log.WarningLog.Printf("worktree at %s is orphaned; skipping dirty check and remove",
			i.gitWorktree.GetWorktreePath())
		if err := i.tmuxSession.DetachSafely(); err != nil {
			errs = append(errs, fmt.Errorf("failed to detach tmux session: %w", err))
			log.ErrorLog.Print(err)
		}
		// Drop any leftover directory so a future Resume's `git worktree add` won't conflict.
		if err := os.RemoveAll(i.gitWorktree.GetWorktreePath()); err != nil {
			errs = append(errs, fmt.Errorf("failed to remove orphaned worktree directory: %w", err))
			log.ErrorLog.Print(err)
		}
		if err := i.gitWorktree.Prune(); err != nil {
			errs = append(errs, fmt.Errorf("failed to prune git worktrees: %w", err))
			log.ErrorLog.Print(err)
		}
		i.SetStatus(Paused)
		_ = clipboard.WriteAll(i.gitWorktree.GetBranchName())
		return i.combineErrors(errs)
	}

	// Check if there are any changes to commit
	if dirty, err := i.gitWorktree.IsDirty(); err != nil {
		errs = append(errs, fmt.Errorf("failed to check if worktree is dirty: %w", err))
		log.ErrorLog.Print(err)
	} else if dirty {
		// Commit changes locally (without pushing to GitHub)
		commitMsg := fmt.Sprintf("[claudesquad] update from '%s' on %s (paused)", i.Title, time.Now().Format(time.RFC822))
		if err := i.gitWorktree.CommitChanges(commitMsg); err != nil {
			errs = append(errs, fmt.Errorf("failed to commit changes: %w", err))
			log.ErrorLog.Print(err)
			// Return early if we can't commit changes to avoid corrupted state
			return i.combineErrors(errs)
		}
	}

	// Detach from tmux session instead of closing to preserve session output
	if err := i.tmuxSession.DetachSafely(); err != nil {
		errs = append(errs, fmt.Errorf("failed to detach tmux session: %w", err))
		log.ErrorLog.Print(err)
		// Continue with pause process even if detach fails
	}

	// Check if worktree exists before trying to remove it
	if _, err := os.Stat(i.gitWorktree.GetWorktreePath()); err == nil {
		// Remove worktree but keep branch
		if err := i.gitWorktree.Remove(); err != nil {
			errs = append(errs, fmt.Errorf("failed to remove git worktree: %w", err))
			log.ErrorLog.Print(err)
			return i.combineErrors(errs)
		}

		// Only prune if remove was successful
		if err := i.gitWorktree.Prune(); err != nil {
			errs = append(errs, fmt.Errorf("failed to prune git worktrees: %w", err))
			log.ErrorLog.Print(err)
			return i.combineErrors(errs)
		}
	}

	i.SetStatus(Paused)
	_ = clipboard.WriteAll(i.gitWorktree.GetBranchName())

	if err := i.combineErrors(errs); err != nil {
		log.ErrorLog.Print(err)
		return err
	}
	return nil
}

// claudeSessionFile is where the notification hook records the Claude Code
// conversation id of the agent running in a tmux session.
func claudeSessionFile(tmuxName string) string {
	dir, err := config.GetConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "claude-sessions", tmuxName)
}

// ClaudeConversationID returns the recorded Claude Code conversation id, if any.
func (i *Instance) ClaudeConversationID() string {
	if i.tmuxSession == nil {
		return ""
	}
	path := claudeSessionFile(i.tmuxSession.Name())
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	id := strings.TrimSpace(string(data))
	if !conversationIDRe.MatchString(id) {
		return ""
	}
	return id
}

var conversationIDRe = regexp.MustCompile(`^[0-9a-fA-F-]{8,64}$`)

// startAgent starts the agent in a new tmux session. When the Claude conversation
// this session had is known, it is resumed exactly instead of starting fresh.
func (i *Instance) startAgent() error {
	// A worker started (or woken, restarted) without the worker permissions gets them
	// now: sessions opened before 1.15 pick them up on cs ctl restart / wake.
	if !i.Conductor && !i.Assistant && i.runsClaude() && !strings.Contains(i.Program, "--settings") {
		EnsureWorkerSettings()
		i.Program += workerSettingsArg(i.Program)
	}
	cmd := i.Program
	if id := i.ClaudeConversationID(); id != "" && i.runsClaude() {
		// Squad's wrapper scripts pass --resume through (and skip their own
		// --continue), so the same program resumes the exact conversation.
		cmd = fmt.Sprintf("%s --resume %s", i.Program, id)
	}
	return i.tmuxSession.StartWithCommand(i.gitWorktree.GetWorktreePath(), cmd)
}

// RestartAgent restarts the agent process with the same conversation, e.g. to
// reconnect its MCP servers. Refused while it works or when the conversation
// could not be resumed exactly.
func (i *Instance) RestartAgent() error {
	if i.Status == Running || i.Status == Loading {
		return fmt.Errorf("«%s» сейчас работает — перезапуск оборвал бы шаг", i.Title)
	}
	if i.Status != Paused {
		if err := i.Hibernate(); err != nil {
			return err
		}
		// tmux removes a killed session asynchronously: wait until it is gone, or
		// Resume would try to reattach to the dying one.
		for k := 0; k < 30 && i.tmuxSession.DoesSessionExist(); k++ {
			time.Sleep(100 * time.Millisecond)
		}
	}
	return i.Resume()
}

// ReleaseDeadSession frees the handle to a tmux session that no longer exists.
func (i *Instance) ReleaseDeadSession() {
	if i.tmuxSession != nil {
		_ = i.tmuxSession.Close() // kill fails harmlessly; the PTY handle is closed
	}
}

// runsClaude reports whether the session's program is Claude Code (a path or
// wrapper ending in "claude", possibly followed by flags).
func (i *Instance) runsClaude() bool {
	fields := strings.Fields(i.Program)
	return len(fields) > 0 && filepath.Base(fields[0]) == tmux.ProgramClaude
}

// Hibernate stops the agent process to free memory. The worktree, the branch and
// the conversation stay; Resume starts the same conversation again.
func (i *Instance) Hibernate() error {
	if !i.started || i.Status == Paused {
		return nil
	}
	if i.ClaudeConversationID() == "" || !i.runsClaude() {
		return fmt.Errorf("не смогу поднять тот же разговор «%s», усыплять небезопасно", i.Title)
	}
	if err := i.tmuxSession.Close(); err != nil {
		return err
	}
	i.Hibernated = true
	i.SetStatus(Paused)
	return nil
}

// Rename changes the title and the tmux session name together.
func (i *Instance) Rename(newTitle string) error {
	newTitle = strings.TrimSpace(newTitle)
	if newTitle == "" {
		return fmt.Errorf("название не может быть пустым")
	}
	if i.tmuxSession != nil {
		oldFile := claudeSessionFile(i.tmuxSession.Name())
		if err := i.tmuxSession.Rename(newTitle); err != nil {
			return err
		}
		if oldFile != "" {
			_ = os.Rename(oldFile, claudeSessionFile(i.tmuxSession.Name()))
		}
	}
	i.Title = newTitle
	return nil
}

// Resume recreates the worktree and restarts the tmux session
func (i *Instance) Resume() error {
	if !i.started {
		return fmt.Errorf("cannot resume instance that has not been started")
	}
	if i.Status != Paused {
		return fmt.Errorf("can only resume paused instances")
	}

	// Check if branch is checked out
	if checked, err := i.gitWorktree.IsBranchCheckedOut(); err != nil {
		log.ErrorLog.Print(err)
		return fmt.Errorf("failed to check if branch is checked out: %w", err)
	} else if checked {
		return fmt.Errorf("cannot resume: branch is checked out, please switch to a different branch")
	}

	// Setup git worktree. Setup removes and re-adds the worktree from the branch, which
	// throws away anything uncommitted in it. After a normal Pause the directory is gone
	// and that is exactly what we want; but an instance paused because its tmux session
	// died still has its worktree — and the work in it — sitting on disk, so leave it be.
	if valid, err := i.gitWorktree.IsValidWorktree(); err != nil || !valid {
		if err != nil {
			log.WarningLog.Printf("could not validate worktree at %s, recreating it: %v",
				i.gitWorktree.GetWorktreePath(), err)
		}
		if err := i.gitWorktree.Setup(); err != nil {
			log.ErrorLog.Print(err)
			return fmt.Errorf("failed to setup git worktree: %w", err)
		}
	}

	// Check if tmux session still exists from pause, otherwise create new one
	i.tmuxSession.AdoptLegacySession(i.Title)
	restored := false
	if i.tmuxSession.DoesSessionExist() {
		// Session exists, just restore PTY connection to it. It may vanish right
		// now (it was just closed for a restart): then start a new one below.
		if err := i.tmuxSession.Restore(); err == nil {
			restored = true
		} else if i.tmuxSession.DoesSessionExist() {
			log.ErrorLog.Print(err)
			return fmt.Errorf("could not reconnect to the running session: %w", err)
		}
	}
	if !restored {
		// Create new tmux session
		if err := i.startAgent(); err != nil {
			log.ErrorLog.Print(err)
			// Never clean up here: the worktree may hold the session's uncommitted work.
			return fmt.Errorf("failed to start new session: %w", err)
		}
	}

	i.Hibernated = false
	i.resumedAt = time.Now()
	i.SetStatus(Running)
	return nil
}

// UpdateDiffStats updates the git diff statistics for this instance
func (i *Instance) UpdateDiffStats() error {
	if !i.started {
		i.diffStats = nil
		return nil
	}

	if i.Status == Paused {
		// Keep the previous diff stats if the instance is paused
		return nil
	}

	stats := i.gitWorktree.Diff()
	if stats.Error != nil {
		if strings.Contains(stats.Error.Error(), "base commit SHA not set") {
			// Worktree is not fully set up yet, not an error
			i.diffStats = nil
			return nil
		}
		return fmt.Errorf("failed to get diff stats: %w", stats.Error)
	}

	i.diffStats = stats
	return nil
}

// ComputeDiff runs the expensive git diff I/O and returns the result without
// mutating instance state. Safe to call from a background goroutine.
func (i *Instance) ComputeDiff() *git.DiffStats {
	if !i.started || i.Status == Paused {
		return nil
	}
	return i.gitWorktree.Diff()
}

// ComputeDiffNumstat runs a lightweight git diff --numstat and returns only the
// added/removed line counts (Content is left empty). Safe to call from a
// background goroutine. Use this for instances whose full diff content is not
// currently needed so we avoid keeping large diffs in memory.
func (i *Instance) ComputeDiffNumstat() *git.DiffStats {
	if !i.started || i.Status == Paused {
		return nil
	}
	return i.gitWorktree.DiffNumstat()
}

// SetDiffStats sets the diff statistics on the instance. Should be called from
// the main event loop to avoid data races with View.
func (i *Instance) SetDiffStats(stats *git.DiffStats) {
	i.diffStats = stats
}

// GetDiffStats returns the current git diff statistics
func (i *Instance) GetDiffStats() *git.DiffStats {
	return i.diffStats
}

// SendPrompt sends a prompt to the tmux session
func (i *Instance) SendPrompt(prompt string) error {
	if !i.started {
		return fmt.Errorf("instance not started")
	}
	if i.tmuxSession == nil {
		return fmt.Errorf("tmux session not initialized")
	}
	if err := i.tmuxSession.SendKeys(prompt); err != nil {
		return fmt.Errorf("error sending keys to tmux session: %w", err)
	}

	// Wait until the text is in the input box before pressing Enter: right after a
	// start Claude Code can show it a few seconds late, and an Enter pressed before
	// that is lost (the text then sits in the box unsent).
	typed := false
	for k := 0; k < 40 && !typed; k++ { // up to 6 s
		time.Sleep(150 * time.Millisecond)
		if content, err := i.tmuxSession.CapturePaneContent(); err == nil && extractDraft(content) != "" {
			typed = true
		}
	}
	_ = typed // not shown in 6 s: press Enter anyway and watch the box below
	if err := i.tmuxSession.TapEnter(); err != nil {
		return fmt.Errorf("error tapping enter: %w", err)
	}
	// Claude Code sometimes swallows the Enter (still starting, busy redrawing):
	// if the text is still in the box, press Enter once more.
	for attempt := 0; attempt < 2; attempt++ {
		// Watch the box for 2 s, not only the first moment: text that shows up late
		// (after the Enter) would otherwise stay there unsent.
		left := ""
		for k := 0; k < 8; k++ {
			time.Sleep(250 * time.Millisecond)
			content, err := i.tmuxSession.CapturePaneContent()
			if err != nil {
				return nil
			}
			left = extractDraft(content)
		}
		if left == "" {
			return nil
		}
		if attempt == 1 {
			return fmt.Errorf("«%s»: текст остался в поле ввода, Enter не сработал", i.Title)
		}
		if err := i.tmuxSession.TapEnter(); err != nil {
			return fmt.Errorf("error tapping enter: %w", err)
		}
	}
	return nil
}

// PreviewFullHistory captures the entire tmux pane output including full scrollback history
func (i *Instance) PreviewFullHistory() (string, error) {
	if !i.started || i.Status == Paused {
		return "", nil
	}
	return i.tmuxSession.CapturePaneContentWithOptions("-", "-")
}

// SetTmuxSession sets the tmux session for testing purposes
func (i *Instance) SetTmuxSession(session *tmux.TmuxSession) {
	i.tmuxSession = session
}

// SendKeys sends keys to the tmux session
func (i *Instance) SendKeys(keys string) error {
	if !i.started || i.Status == Paused {
		return fmt.Errorf("cannot send keys to instance that has not been started or is paused")
	}
	return i.tmuxSession.SendKeys(keys)
}

// Screen captures the agent's visible screen now ("" when it cannot).
func (i *Instance) Screen() string {
	if !i.started || i.tmuxSession == nil {
		return ""
	}
	out, err := i.tmuxSession.CapturePaneContent()
	if err != nil {
		return ""
	}
	return out
}

// PressKeys types raw keys into the agent's terminal (menu answers).
func (i *Instance) PressKeys(keys string) error {
	if !i.started || i.tmuxSession == nil {
		return fmt.Errorf("сессия не запущена")
	}
	return i.tmuxSession.SendKeys(keys)
}
