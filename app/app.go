package app

import (
	"claude-squad/config"
	"claude-squad/keys"
	"claude-squad/log"
	"claude-squad/session"
	"claude-squad/session/git"
	"claude-squad/session/tmux"
	"claude-squad/ui"
	"claude-squad/ui/overlay"
	"context"
	"fmt"
	"github.com/atotto/clipboard"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// GlobalInstanceLimit caps sessions created with n/N (the conductor's cs ctl new is not
// limited): 10 was too few with a conductor, an assistant and a dozen workers.
const GlobalInstanceLimit = 40

// Run is the main entrypoint into the application.
func Run(ctx context.Context, program string, autoYes bool) error {
	release, err := claimSingleInstance()
	if err != nil {
		// Exit code 3 tells the sq wrapper not to restart us (another Squad is running).
		fmt.Fprintln(os.Stderr, err)
		os.Exit(3)
	}
	defer release()
	h := newHome(ctx, program, autoYes)
	// The sq wrapper stops Squad with SIGTERM to update it. While the user is inside a
	// session the UI loop is blocked and would never see it: leave the session first,
	// then bubbletea quits as usual.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM)
	defer signal.Stop(sigs)
	go func() {
		for range sigs {
			h.mu.Lock()
			detach := h.detach
			h.mu.Unlock()
			if detach != nil {
				detach()
			}
		}
	}()
	p := tea.NewProgram(
		h,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(), // Mouse scroll
	)
	_, err = p.Run()
	return err
}

type state int

const (
	stateDefault state = iota
	// stateNew is the state when the user is creating a new instance.
	stateNew
	// statePrompt is the state when the user is entering a prompt.
	statePrompt
	// stateHelp is the state when a help screen is displayed.
	stateHelp
	// stateConfirm is the state when a confirmation modal is displayed.
	stateConfirm
)

type home struct {
	ctx context.Context

	// -- Storage and Configuration --

	program string
	autoYes bool

	// storage is the interface for saving/loading data to/from the app's state
	storage *session.Storage
	// appConfig stores persistent application configuration
	appConfig *config.Config
	// appState stores persistent application state like seen help screens
	appState config.AppState

	// -- State --

	// state is the current discrete state of the application
	state state
	// newInstanceFinalizer is called when the state is stateNew and then you press enter.
	// It registers the new instance in the list after the instance has been started.
	newInstanceFinalizer func()

	// promptAfterName tracks if we should enter prompt mode after naming
	promptAfterName bool

	// editingNote is true while the prompt overlay is used to edit a session description
	editingNote bool
	// renaming is true while the prompt overlay is used to rename a session
	renaming bool
	// replyTarget is the session a quick reply (without attaching) is being written to
	replyTarget *session.Instance
	// overview shows all sessions at once; overviewIdx is the highlighted one
	overview    bool
	overviewIdx int
	// briefNotified: today's morning brief was shown as a Mac notification
	briefNotified bool
	briefDay      string    // the day the brief went out
	briefTriedAt  time.Time // last attempt (the conductor may be busy)
	// updateNoted: the conductor knows about the current Squad version
	updateNoted bool
	// lastDiskNoteAt: when the digest last warned about free disk space
	lastDiskNoteAt time.Time
	// lastMRNoteAt: when the digest last carried the MR round
	lastMRNoteAt time.Time
	// queue: the «ждут тебя» screen; queueIdx is the selected row
	queue    bool
	queueIdx int
	// replyPrefill is put into the next reply box (when the target has no draft)
	replyPrefill string
	// last known window size
	winW, winH int
	// mu guards everything below and the sessions; the background loop shares it
	mu     sync.Mutex
	bgOnce sync.Once
	// lastMetaAt is when the UI loop last processed a monitoring round
	lastMetaAt time.Time
	// uiWaiting says what blocks the UI (e.g. the user is inside a session)
	uiWaiting string
	// attached is the session the user is inside right now (never touched meanwhile)
	attached *session.Instance
	// detach leaves the full-screen session the user is in (agent or terminal tab).
	detach func()
	// conductor bookkeeping
	lastSnapshotAt time.Time
	lastDigestAt   time.Time
	lastDigest     map[int]string
	inboxMu        sync.Mutex
	inboxRunning   bool
	lastInboxAt    time.Time
	pendingInbox   []string
	// focusTarget is a session to open full screen (notification click)
	focusTarget *session.Instance
	focusUntil  time.Time
	// status history and the morning report (see report.go)
	statusLog         map[*session.Instance]*statusTrack
	morningCheckedDay string
	morningPending    bool
	// token limits (see usage.go)
	usage usageState
	// overview pet (see pet.go)
	density          float64
	hasDensity       bool
	densityTracker   *session.DensityTracker
	t39              float64
	hasT39           bool
	limitTracks      map[*session.Instance]*limitTrack
	digestSentAt     time.Time
	digestAcked      bool
	digestWarned     bool
	densityCmdAt     time.Time
	densityCmdValue  float64
	densityCmdOK     bool
	densityCmdNow    int               // commit in the current hour: -1 unknown, 0 no, 1 yes
	densityCmdHour   string            // the hour ("2006-01-02 15") densityCmdNow is about
	diffErrLogged    map[string]string // last diff-stats error logged per session
	petPhrases       ui.PetPhrases     // your own pet phrases (pet-phrases.json)
	emptyHourSent    string
	wipesReadAt      time.Time
	densityRunAt     time.Time
	densityRunning   bool
	lastCapacityNote time.Time
	// VPN watch (see netwatch.go)
	net        netProbe
	netDown    bool
	netUpAt    time.Time
	netRecover map[*session.Instance]*netRecovery
	netActions []string

	// keySent is used to manage underlining menu items
	keySent bool

	// instanceStarting is true while a background instance start is in progress.
	// Prevents double-submission and guards against interacting with a not-yet-started instance.
	instanceStarting bool
	// startingInstance holds a reference to the instance being started in the background.
	startingInstance *session.Instance

	// -- UI Components --

	// list displays the list of instances
	list *ui.List
	// menu displays the bottom menu
	menu *ui.Menu
	// tabbedWindow displays the tabbed window with preview and diff panes
	tabbedWindow *ui.TabbedWindow
	// errBox displays error messages
	errBox *ui.ErrBox
	// global spinner instance. we plumb this down to where it's needed
	spinner spinner.Model
	// textInputOverlay handles text input with state
	textInputOverlay *overlay.TextInputOverlay
	// textOverlay displays text information
	textOverlay *overlay.TextOverlay
	// confirmationOverlay displays confirmation modals
	confirmationOverlay *overlay.ConfirmationOverlay
}

func newHome(ctx context.Context, program string, autoYes bool) *home {
	// Load application config
	appConfig := config.LoadConfig()

	// Load application state
	appState := config.LoadState()

	// Initialize storage
	storage, err := session.NewStorage(appState)
	if err != nil {
		fmt.Printf("Failed to initialize storage: %v\n", err)
		os.Exit(1)
	}

	h := &home{
		ctx:          ctx,
		spinner:      spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		menu:         ui.NewMenu(),
		tabbedWindow: ui.NewTabbedWindow(ui.NewPreviewPane(), ui.NewDiffPane(), ui.NewTerminalPane()),
		errBox:       ui.NewErrBox(),
		storage:      storage,
		appConfig:    appConfig,
		program:      program,
		autoYes:      autoYes,
		state:        stateDefault,
		appState:     appState,
	}
	h.list = ui.NewList(&h.spinner, autoYes)

	// Load saved instances
	instances, err := storage.LoadInstances()
	if err != nil {
		fmt.Printf("Failed to load instances: %v\n", err)
		os.Exit(1)
	}

	// Add loaded instances to the list
	for _, instance := range instances {
		// Call the finalizer immediately.
		h.list.AddInstance(instance)()
		if autoYes {
			instance.AutoYes = true
		}
	}

	return h
}

// updateHandleWindowSizeEvent sets the sizes of the components.
// The components will try to render inside their bounds.
// workingRe: the agent is working right now — Claude Code's spinner line or the
// "esc to interrupt" hint (Codex too).
var workingRe = regexp.MustCompile(`(?m)^\s*[·✢✳✶✻✽*]\s+\S+…\s*\(|esc to interrupt`)

func (m *home) updateHandleWindowSizeEvent(msg tea.WindowSizeMsg) {
	m.winW, m.winH = msg.Width, msg.Height
	// List takes 30% of width, preview takes 70%
	listWidth := int(float32(msg.Width) * 0.3)
	tabsWidth := msg.Width - listWidth

	// Menu takes 10% of height, list and window take 90%
	contentHeight := int(float32(msg.Height) * 0.9)
	menuHeight := msg.Height - contentHeight - 1     // minus 1 for error box
	m.errBox.SetSize(int(float32(msg.Width)*0.9), 1) // error box takes 1 row

	m.tabbedWindow.SetSize(tabsWidth, contentHeight)
	m.list.SetSize(listWidth, contentHeight)

	if m.textInputOverlay != nil {
		if m.replyTarget != nil {
			// Quick reply shows the recent conversation, so give it more room.
			m.textInputOverlay.SetSize(int(float32(msg.Width)*0.85), int(float32(msg.Height)*0.4))
		} else {
			m.textInputOverlay.SetSize(int(float32(msg.Width)*0.6), int(float32(msg.Height)*0.4))
		}
	}
	if m.textOverlay != nil {
		m.textOverlay.SetWidth(int(float32(msg.Width) * 0.6))
	}

	previewWidth, previewHeight := m.tabbedWindow.GetPreviewSize()
	if err := m.list.SetSessionPreviewSize(previewWidth, previewHeight); err != nil {
		log.ErrorLog.Print(err)
	}
	m.menu.SetSize(msg.Width, menuHeight)
}

func (m *home) Init() tea.Cmd {
	// Upon starting, we want to start the spinner. Whenever we get a spinner.TickMsg, we
	// update the spinner, which sends a new spinner.TickMsg. I think this lasts forever lol.
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			time.Sleep(100 * time.Millisecond)
			return previewTickMsg{}
		},
		tickUpdateMetadataCmd(m.snapshotActiveInstances(), m.list.GetSelectedInstance()),
	)
}

// Update is bubbletea's entry point. Everything that touches sessions runs under
// m.mu, shared with the background loop.
func (m *home) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bgOnce.Do(func() {
		m.setupNetwork()
		go m.backgroundLoop()
		go m.startWeb()
	})
	return m.update(msg)
}

func (m *home) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case hideErrMsg:
		m.errBox.Clear()
	case previewTickMsg:
		cmd := m.instanceChanged()
		return m, tea.Batch(
			cmd,
			func() tea.Msg {
				time.Sleep(100 * time.Millisecond)
				return previewTickMsg{}
			},
		)
	case keyupMsg:
		m.menu.ClearKeydown()
		return m, nil
	case instanceStartDoneMsg:
		m.instanceStarting = false
		inst := msg.instance
		m.startingInstance = nil

		if msg.err != nil {
			// Start failed — remove the instance from the list and show the error.
			m.list.Kill()
			return m, tea.Batch(tea.WindowSize(), m.instanceChanged(), m.handleError(msg.err))
		}

		// Save after successful start.
		if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
			return m, m.handleError(err)
		}

		if m.promptAfterName {
			m.state = statePrompt
			m.menu.SetState(ui.StatePrompt)
			m.textInputOverlay = overlay.NewTextInputOverlay("Enter prompt", "")
			m.promptAfterName = false
		} else {
			m.showHelpScreen(helpStart(inst), nil)
		}

		return m, tea.Batch(tea.WindowSize(), m.instanceChanged())
	case metadataUpdateDoneMsg:
		m.processMetadata(msg.results)
		m.lastMetaAt = time.Now()
		next := tickUpdateMetadataCmd(m.snapshotActiveInstances(), m.list.GetSelectedInstance())
		m.maybeMorningReport()
		m.maybeMorningBrief(time.Now())
		if enter := m.takeFocus(); enter != nil {
			return m, tea.Batch(next, enter)
		}
		return m, next
	case tea.MouseMsg:
		// Handle mouse wheel events for scrolling the diff/preview pane
		if msg.Action == tea.MouseActionPress {
			if msg.Button == tea.MouseButtonWheelDown || msg.Button == tea.MouseButtonWheelUp {
				selected := m.list.GetSelectedInstance()
				if selected == nil || selected.Status == session.Paused {
					return m, nil
				}

				switch msg.Button {
				case tea.MouseButtonWheelUp:
					m.tabbedWindow.ScrollUp()
				case tea.MouseButtonWheelDown:
					m.tabbedWindow.ScrollDown()
				}
			}
		}
		return m, nil
	case branchSearchDebounceMsg:
		// Debounce timer fired — check if this is still the current filter version
		if m.textInputOverlay == nil {
			return m, nil
		}
		if msg.version != m.textInputOverlay.BranchFilterVersion() {
			return m, nil // stale, a newer debounce is pending
		}
		return m, m.runBranchSearch(msg.filter, msg.version)
	case branchSearchResultMsg:
		if m.textInputOverlay != nil {
			m.textInputOverlay.SetBranchResults(msg.branches, msg.version)
		}
		return m, nil
	case tea.KeyMsg:
		if m.morningPending && m.state == stateDefault && !m.overview {
			// The first key after the night opens "что было": the user is back.
			m.morningPending = false
			if m.showMorningReport() {
				return m, nil
			}
		}
		return m.handleKeyPress(msg)
	case tea.WindowSizeMsg:
		m.updateHandleWindowSizeEvent(msg)
		return m, nil
	case error:
		// Handle errors from confirmation actions
		return m, m.handleError(msg)
	case instanceChangedMsg:
		// Handle instance changed after confirmation action
		return m, m.instanceChanged()
	case instanceStartedMsg:
		// Select the instance that just started (or failed)
		m.list.SelectInstance(msg.instance)

		if msg.err != nil {
			m.list.Kill()
			return m, tea.Batch(m.handleError(msg.err), m.instanceChanged())
		}

		// Save after successful start
		if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
			return m, m.handleError(err)
		}
		if m.autoYes {
			msg.instance.AutoYes = true
		}

		if msg.promptAfterName {
			m.state = statePrompt
			m.menu.SetState(ui.StatePrompt)
			m.textInputOverlay = m.newPromptOverlay()
		} else {
			// If instance has a prompt (set from Shift+N flow), send it now
			if msg.instance.Prompt != "" {
				if err := msg.instance.SendPrompt(msg.instance.Prompt); err != nil {
					log.ErrorLog.Printf("failed to send prompt: %v", err)
				}
				msg.instance.Prompt = ""
			}
			m.menu.SetState(ui.StateDefault)
			m.showHelpScreen(helpStart(msg.instance), nil)
		}

		return m, tea.Batch(tea.WindowSize(), m.instanceChanged())
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *home) handleQuit() (tea.Model, tea.Cmd) {
	if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
		return m, m.handleError(err)
	}
	return m, tea.Quit
}

func (m *home) handleMenuHighlighting(msg tea.KeyMsg) (cmd tea.Cmd, returnEarly bool) {
	// Handle menu highlighting when you press a button. We intercept it here and immediately return to
	// update the ui while re-sending the keypress. Then, on the next call to this, we actually handle the keypress.
	if m.keySent {
		m.keySent = false
		return nil, false
	}
	if m.state == statePrompt || m.state == stateHelp || m.state == stateConfirm {
		return nil, false
	}
	// If it's in the global keymap, we should try to highlight it.
	name, ok := keys.GlobalKeyStringsMap[msg.String()]
	if !ok {
		return nil, false
	}

	if m.list.GetSelectedInstance() != nil && m.list.GetSelectedInstance().Paused() && name == keys.KeyEnter {
		return nil, false
	}
	if name == keys.KeyShiftDown || name == keys.KeyShiftUp {
		return nil, false
	}

	// Skip the menu highlighting if the key is not in the map or we are using the shift up and down keys.
	// TODO: cleanup: when you press enter on stateNew, we use keys.KeySubmitName. We should unify the keymap.
	if name == keys.KeyEnter && m.state == stateNew {
		name = keys.KeySubmitName
	}
	m.keySent = true
	return tea.Batch(
		func() tea.Msg { return msg },
		m.keydownCallback(name)), true
}

// waitDetached blocks until the user leaves a full-screen session. The lock is
// released meanwhile so the background loop keeps monitoring sessions, serving
// cs ctl requests and sending conductor digests.
func (m *home) waitDetached(ch chan struct{}, what string, agent bool, detach func()) {
	m.uiWaiting = what
	m.detach = detach
	if agent {
		// Inside the agent's own session: Squad must not type into it meanwhile.
		m.attached = m.list.GetSelectedInstance()
		if m.attached != nil {
			m.attached.CallsUserSeen = m.attached.CallsUser // the user came: the call is answered
		}
	}
	m.mu.Unlock()
	<-ch
	m.mu.Lock()
	m.uiWaiting = ""
	m.attached = nil
	m.detach = nil
}

// uiWaitingDesc describes what the UI is waiting for, for cs ctl.
func (m *home) uiWaitingDesc() string {
	if m.uiWaiting != "" {
		return m.uiWaiting
	}
	switch m.state {
	case stateConfirm:
		return "в Squad открыто окно подтверждения"
	case statePrompt:
		return "в Squad открыто поле ввода"
	case stateHelp:
		return "в Squad открыто окно с текстом"
	}
	return ""
}

// backgroundLoop keeps monitoring going while the UI loop is blocked (bubbletea
// does not call Update while the user is inside a session full screen).
func (m *home) backgroundLoop() {
	for {
		time.Sleep(2 * time.Second)
		m.mu.Lock()
		if time.Since(m.lastMetaAt) < 4*time.Second {
			m.mu.Unlock()
			continue
		}
		active := m.snapshotActiveInstances()
		selected := m.list.GetSelectedInstance()
		m.mu.Unlock()

		results := collectMetadata(active, selected)

		m.mu.Lock()
		if time.Since(m.lastMetaAt) >= 4*time.Second {
			m.processMetadata(results)
			m.lastMetaAt = time.Now()
		}
		m.mu.Unlock()
	}
}

// assignNumbers gives every session a stable number (shown in the list and in
// notifications) that does not change when the list is re-sorted.
func (m *home) assignNumbers() bool {
	used := map[int]bool{}
	maxN := 0
	for _, inst := range m.list.GetInstances() {
		if inst.Number > 0 && !used[inst.Number] {
			used[inst.Number] = true
			if inst.Number > maxN {
				maxN = inst.Number
			}
		} else {
			inst.Number = 0
		}
	}
	// Never hand out a number that was used before: a conductor may still hold it.
	if last := session.LastIssuedNumber(); last > maxN {
		maxN = last
	}
	changed := false
	for _, inst := range m.list.GetInstances() {
		if inst.Number == 0 && inst.Started() {
			maxN++
			inst.Number = maxN
			changed = true
		}
	}
	if changed {
		session.SetLastIssuedNumber(maxN)
	}
	return changed
}

// deliverPendingPrompt sends a new session's first message once the agent shows
// its input box (typing into a still-starting agent would lose keystrokes).
func (m *home) deliverPendingPrompt(inst *session.Instance) {
	// trust_folders: only a session Squad itself just opened for the conductor and that
	// still waits with its first task — never any other screen that shows the phrase.
	if inst.TrustPrompt && m.appConfig.TrustFolders && inst != m.attached && inst.ByConductor && inst.PendingPrompt != "" &&
		time.Since(inst.CreatedAt) < 10*time.Minute {
		if inst.CheckAndHandleTrustPrompt() {
			log.InfoLog.Printf("%q: trusted the new folder (trust_folders)", inst.Title)
		}
		return
	}
	if inst.PendingPrompt == "" || inst.Status != session.Ready || !inst.InputReady() || inst.TrustPrompt {
		return
	}
	if err := inst.ReplaceInput(inst.PendingPrompt); err != nil {
		// Keep the task and try again on a later tick: the agent may still be starting.
		inst.PendingTries++
		log.WarningLog.Printf("first prompt for %q (try %d): %v", inst.Title, inst.PendingTries, err)
		if inst.PendingTries >= 5 {
			notifyUser(fmt.Sprintf("«%s»: первое задание не доставлено", inst.Title), "Зайди в сессию и отправь его вручную; текст — в логе Squad")
			log.WarningLog.Printf("first prompt for %q given up: %s", inst.Title, inst.PendingPrompt)
			inst.PendingPrompt, inst.PendingTries = "", 0
			m.savePending()
		}
		return
	}
	inst.PendingPrompt, inst.PendingTries = "", 0
	// Saved now: a restart (sq update, crash) must not send the task a second time.
	m.savePending()
}

func (m *home) savePending() {
	if m.storage == nil {
		return
	}
	if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
		log.WarningLog.Printf("could not save after the first prompt: %v", err)
	}
}

// runDailyJobs queues each configured daily job once a day inside its window.
func (m *home) runDailyJobs() {
	if len(m.appConfig.DailyJobs) == 0 {
		return
	}
	dir, err := config.GetConfigDir()
	if err != nil {
		return
	}
	now := time.Now()
	today := now.Format("2006-01-02")
	for _, job := range m.appConfig.DailyJobs {
		if job.Name == "" || !job.DueToday(now) {
			continue
		}
		stamp := filepath.Join(dir, "daily-jobs", job.Name)
		if data, err := os.ReadFile(stamp); err == nil && strings.TrimSpace(string(data)) == today {
			continue
		}
		title := strings.TrimSpace(job.Title + " " + now.Format("02.01"))
		if _, err := session.SubmitSessionRequest(session.SessionRequest{
			Title: title, Dir: job.Dir, Prompt: job.Prompt, Color: "cyan",
			Note: "ежедневная задача " + job.Name,
		}); err != nil {
			log.WarningLog.Printf("daily job %s: %v", job.Name, err)
			continue
		}
		_ = os.MkdirAll(filepath.Dir(stamp), 0700)
		_ = os.WriteFile(stamp, []byte(today), 0600)
	}
}

// takeFocus opens the session a notification was clicked for, once it is up and
// nothing else is open in Squad. Runs on the UI goroutine.
func (m *home) takeFocus() tea.Cmd {
	inst := m.focusTarget
	if inst == nil {
		return nil
	}
	if time.Now().After(m.focusUntil) {
		m.focusTarget = nil
		return nil
	}
	if m.state != stateDefault || m.overview || inst.Paused() || inst.Status == session.Loading || !inst.TmuxAlive() {
		return nil // try again next tick
	}
	m.focusTarget = nil
	m.list.SelectInstance(inst)
	m.instanceChanged()
	return func() tea.Msg { return tea.KeyMsg{Type: tea.KeyEnter} }
}

// takeControlRequests handles wake/restart/focus requests. They do not change the
// list, so they run even while a window is open in Squad or the user is inside
// another session. A request for the session the user is inside waits.
func (m *home) takeControlRequests() bool {
	reqs := session.TakeSessionRequestsWhere(func(r session.SessionRequest) bool {
		if !(r.Wake || r.Restart || r.Focus || r.Mark != "" || r.MR != "" || r.SetGroup) {
			return false
		}
		return m.attached == nil || m.attached.Title != r.Title || r.Focus || r.Mark != "" || r.MR != "" || r.SetGroup
	})
	return m.applyRequests(reqs)
}

// takeSessionRequests opens/closes sessions (cs ctl new/kill); only when no
// window is open, so the list never changes under the user.
func (m *home) takeSessionRequests() bool {
	return m.applyRequests(session.TakeSessionRequestsWhere(func(r session.SessionRequest) bool {
		return !(r.Wake || r.Restart || r.Focus || r.Mark != "" || r.MR != "" || r.SetGroup)
	}))
}

func (m *home) applyRequests(reqs []session.SessionRequest) bool {
	created := false
	var newOnes []*session.Instance
	for _, req := range reqs {
		if req.Focus {
			for _, inst := range m.list.GetInstances() {
				if inst.Title == req.Title {
					if inst.Paused() {
						if err := inst.Resume(); err != nil {
							log.WarningLog.Printf("focus %q: %v", req.Title, err)
							continue
						}
					}
					m.focusTarget, m.focusUntil = inst, time.Now().Add(30*time.Second)
					created = true
				}
			}
			continue
		}
		if req.SetGroup {
			found := false
			for _, inst := range m.list.GetInstances() {
				if inst.Title == req.Title {
					found = true
					inst.Group = req.Group
					session.WriteCtlResult(req.Title, "ок, группа: "+inst.GroupOf())
					created = true
				}
			}
			if !found {
				session.WriteCtlResult(req.Title, "ошибка: такой сессии нет")
			}
			continue
		}
		if req.MR != "" {
			found := false
			for _, inst := range m.list.GetInstances() {
				if inst.Title != req.Title {
					continue
				}
				found = true
				if inst.SetMRState(req.MR, req.MRState) && inst.Status != session.Running {
					// Everything merged: offer to close the session (the user decides, D).
					inst.MarkedFor, inst.MarkReason = "user", "MR влиты — можно закрыть сессию (D)"
					session.AppendEvent(session.Event{Kind: "status", Number: inst.Number, Title: inst.Title,
						Text: fmt.Sprintf("#%d: все MR влиты — можно закрыть", inst.Number)})
				}
				session.WriteCtlResult(req.Title, "ок")
				created = true // saved below with the list
			}
			if !found {
				session.WriteCtlResult(req.Title, "ошибка: такой сессии нет")
			}
			continue
		}
		if req.Mark != "" {
			found := false
			for _, inst := range m.list.GetInstances() {
				if inst.Title == req.Title {
					found = true
					if req.Mark == "clear" {
						inst.MarkedFor, inst.MarkReason = "", ""
					} else {
						inst.MarkedFor, inst.MarkReason = req.Mark, req.MarkReason
					}
					session.WriteCtlResult(req.Title, "ок")
				}
			}
			if !found {
				session.WriteCtlResult(req.Title, "ошибка: такой сессии нет")
			}
			continue
		}
		if req.Wake || req.Restart {
			for _, inst := range m.list.GetInstances() {
				if inst.Title != req.Title || inst == m.attached {
					continue
				}
				var err error
				switch {
				case req.Restart:
					err = inst.RestartAgent()
				case inst.Paused():
					err = inst.Resume()
				}
				if err != nil {
					log.WarningLog.Printf("ctl %q: %v", req.Title, err)
					session.WriteCtlResult(req.Title, "ошибка: "+err.Error())
				} else {
					session.WriteCtlResult(req.Title, "ок")
				}
				created = true
			}
			continue
		}
		if req.Kill {
			killed := false
			prev := m.list.GetSelectedInstance()
			for _, inst := range m.list.GetInstances() {
				if inst.Title == req.Title && !inst.Conductor && !inst.Assistant && inst != m.attached {
					m.tabbedWindow.CleanupTerminalForInstance(inst.Title)
					if err := m.storage.DeleteInstance(inst.Title); err != nil {
						log.WarningLog.Printf("delete %q: %v", inst.Title, err)
					}
					m.list.SelectInstance(inst)
					m.list.Kill()
					if prev != nil && prev != inst {
						m.list.SelectInstance(prev)
					}
					created = true
					killed = true
					break
				}
			}
			switch {
			case killed:
				session.WriteCtlResult(req.Title, "ок, закрыта")
			case m.attached != nil && m.attached.Title == req.Title:
				session.WriteCtlResult(req.Title, "ошибка: пользователь сейчас внутри этой сессии — не закрываю")
			default:
				session.WriteCtlResult(req.Title, "ошибка: такой сессии нет (или это дирижёр/помощник)")
			}
			continue
		}
		dup := false
		for _, other := range m.list.GetInstances() {
			if other.Title == req.Title {
				dup = true
			}
		}
		if dup {
			log.WarningLog.Printf("request for existing session %q ignored", req.Title)
			session.WriteCtlResult(req.Title, "ошибка: сессия с таким названием уже есть — дай другое название или закрой старую")
			continue
		}
		inst, err := session.NewInstanceInFolder(req, m.program)
		if err != nil {
			log.WarningLog.Printf("session request %q: %v", req.Title, err)
			session.WriteCtlResult(req.Title, "ошибка: "+err.Error())
			continue
		}
		finalize := m.list.AddInstance(inst)
		if err := inst.Start(true); err != nil {
			log.WarningLog.Printf("start %q: %v", req.Title, err)
			m.list.SelectInstance(inst)
			m.list.Kill()
			session.WriteCtlResult(req.Title, "ошибка запуска: "+err.Error())
			continue
		}
		finalize()
		created = true
		newOnes = append(newOnes, inst)
	}
	if created {
		m.assignNumbers()
	}
	for _, inst := range newOnes {
		session.WriteCtlResult(inst.Title, fmt.Sprintf("ок, создана #%d", inst.Number))
	}
	return created
}

// digestKey describes a session for the conductor digest, or ok=false when the
// session is in a state that is not worth waking the conductor for.
func digestKey(inst *session.Instance) (key string, ok bool) {
	label, kind := inst.StatusLabel()
	if inst.Menu != nil && kind == session.StatusConductor {
		// A menu waits for a key press: report it right away, with how to answer.
		return fmt.Sprintf("%s: %s → cs ctl answer %d <номер|recommended|текст>", label, inst.Menu.Summary(), inst.Number), true
	}
	switch kind {
	case session.StatusWorking, session.StatusStarting, session.StatusPaused, session.StatusOnHold, session.StatusStandby:
		return "", false // working or asleep: nothing for the conductor to do
	case session.StatusWaiting, session.StatusConductor:
		// Agents flip between working and waiting all the time; only report a wait
		// that has lasted a few minutes (the agent really finished a step).
		if !inst.IdleSince.IsZero() && time.Since(inst.IdleSince) < 3*time.Minute {
			return "", false
		}
	}
	if stage := inst.Stage(); stage != "" {
		label += " · " + stage
	}
	return label, true
}

// maybeWakeConductor tells the conductor what changed since its last digest.
// Nothing is sent (and no tokens spent) when nothing worth its attention changed.
func (m *home) maybeWakeConductor() {
	interval := m.appConfig.ConductorInterval()
	if interval <= 0 || m.vpnDown() {
		return // nothing the conductor could fix while the VPN is down: save tokens
	}
	var conductor *session.Instance
	for _, inst := range m.list.GetInstances() {
		if inst.Conductor {
			conductor = inst
		}
	}
	if conductor == nil || !conductor.Started() || conductor.Status == session.Loading {
		return
	}
	m.maybeCompactConductor(conductor)
	if m.usageThrottled() {
		interval *= 3 // limits nearly used up: only what matters, less often
	}
	if time.Since(m.lastDigestAt) < interval {
		return
	}
	// Check the chats in the background (the script can take a while); its output
	// waits in pendingInbox until it is delivered, so nothing gets lost.
	m.startInboxCheck()
	if m.lastDigest == nil {
		m.lastDigest = map[int]string{}
	}
	var lines []string
	current := map[int]string{}
	for _, inst := range m.list.GetInstances() {
		if inst.Conductor || inst.Assistant || !inst.Started() {
			continue
		}
		key, ok := digestKey(inst)
		if !ok {
			// Keep what the conductor already knows; report again when it matters.
			if prev, had := m.lastDigest[inst.Number]; had {
				current[inst.Number] = prev
			}
			continue
		}
		current[inst.Number] = key
		if prev, had := m.lastDigest[inst.Number]; !had || prev != key {
			lines = append(lines, fmt.Sprintf("#%d %s — %s", inst.Number, inst.Title, key))
		}
	}
	m.inboxMu.Lock()
	inboxCount := len(m.pendingInbox)
	inbox := strings.Join(m.pendingInbox, " ¶ ")
	inboxBusy := m.inboxRunning
	m.inboxMu.Unlock()
	if note := m.emptyHourNote(); note != "" {
		lines = append(lines, note)
	}
	if note := m.diskNote(time.Now()); note != "" {
		lines = append(lines, note)
	}
	upd := m.updateNote()
	if upd != "" {
		lines = append(lines, upd)
	}
	mrNote := m.mrNote(time.Now())
	if mrNote != "" {
		lines = append(lines, mrNote)
	}
	if len(lines) == 0 && inbox == "" {
		if !inboxBusy {
			m.lastDigestAt = time.Now()
		}
		return
	}
	if conductor.Status == session.Running || conductor.DraftBlocks() || conductor.NeedsInput {
		return // busy or the user is typing to it: try again next tick (an old draft is erased)
	}
	if conductor.Paused() {
		if err := conductor.Resume(); err != nil {
			log.WarningLog.Printf("wake conductor: %v", err)
			m.lastDigestAt = time.Now() // don't hammer a failing resume every second
		}
		return // deliver once it is up
	}
	if !conductor.InputReady() {
		return
	}
	msg := "[squad] Изменения с прошлой сводки: " + strings.Join(lines, "; ")
	if len(lines) == 0 {
		msg = "[squad] Сессии без изменений."
	}
	if inbox != "" {
		msg += " | Новое в чатах: " + inbox
	}
	if err := conductor.ReplaceInput(msg); err != nil {
		log.WarningLog.Printf("digest to conductor: %v", err)
		m.lastDigestAt = time.Now() // back off until the next interval
		return
	}
	m.digestSentAt, m.digestAcked, m.digestWarned = time.Now(), false, false
	if mrNote != "" {
		m.lastMRNoteAt = time.Now()
	}
	if upd != "" {
		m.updateNoteSent()
	}
	m.inboxMu.Lock()
	m.pendingInbox = m.pendingInbox[inboxCount:]
	m.inboxMu.Unlock()
	m.lastDigest = current
	m.lastDigestAt = time.Now()
}

// startInboxCheck runs the chat checker in the background, at most once per interval.
func (m *home) startInboxCheck() {
	m.inboxMu.Lock()
	defer m.inboxMu.Unlock()
	if m.inboxRunning || time.Since(m.lastInboxAt) < m.appConfig.ConductorInterval() {
		return
	}
	m.inboxRunning = true
	m.lastInboxAt = time.Now()
	go func() {
		out := runInboxScript()
		m.inboxMu.Lock()
		if out != "" {
			m.pendingInbox = append(m.pendingInbox, out)
			if len(m.pendingInbox) > 5 {
				m.pendingInbox = m.pendingInbox[len(m.pendingInbox)-5:]
			}
		}
		m.inboxRunning = false
		m.inboxMu.Unlock()
	}()
}

// maybeCompactConductor asks the conductor to compact its conversation once a day
// (from 8:00), so its turns do not get more expensive as the day goes on.
func (m *home) maybeCompactConductor(conductor *session.Instance) {
	if time.Now().Hour() < 8 || conductor.Status != session.Ready || conductor.DraftBlocks() ||
		conductor.NeedsInput || !conductor.InputReady() {
		return
	}
	dir, err := config.GetConfigDir()
	if err != nil {
		return
	}
	stamp := filepath.Join(dir, "conductor", "state", "last-compact")
	today := time.Now().Format("2006-01-02")
	if data, err := os.ReadFile(stamp); err == nil && strings.TrimSpace(string(data)) == today {
		return
	}
	if data, err := os.ReadFile(stamp); err != nil && os.IsNotExist(err) {
		// First run: just remember the day, nothing to compact yet.
		_ = os.MkdirAll(filepath.Dir(stamp), 0700)
		_ = os.WriteFile(stamp, []byte(today), 0600)
		return
	} else if err == nil && len(data) == 0 {
		return
	}
	if err := conductor.ReplaceInput("/compact Сохрани: незакрытые задачи и их сессии (номера), что ждём от пользователя, черновики на подтверждение, счётчики попыток. Остальное подробно есть в journal.md."); err != nil {
		log.WarningLog.Printf("compact conductor: %v", err)
		return
	}
	_ = os.WriteFile(stamp, []byte(today), 0600)
}

// runInboxScript runs the conductor's chat checker if it exists and returns its
// (already filtered) output: only new messages that matter.
func runInboxScript() string {
	dir, err := config.GetConfigDir()
	if err != nil {
		return ""
	}
	script := filepath.Join(dir, "conductor", "bin", "bx-inbox")
	if info, err := os.Stat(script); err != nil || info.Mode()&0111 == 0 {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, script).Output()
	if err != nil {
		log.WarningLog.Printf("bx-inbox: %v", err)
		return ""
	}
	text := strings.TrimSpace(strings.ReplaceAll(string(out), "\n", " ¶ "))
	if len([]rune(text)) > 1500 {
		text = string([]rune(text)[:1500]) + "…"
	}
	return text
}

// urgencyRank orders sessions: the ones that need you first, sleeping last.
func urgencyRank(inst *session.Instance) int {
	if inst.Conductor {
		return -2 // the conductor is always pinned on top
	}
	if inst.Assistant {
		return -1 // the assistant right under it
	}
	_, kind := inst.StatusLabel()
	switch kind {
	case session.StatusNeedsInput:
		return 0
	case session.StatusWaiting:
		if inst.Stage() != "" {
			return 1
		}
		return 2
	case session.StatusConductor, session.StatusWorking, session.StatusOnHold, session.StatusStandby:
		return 3
	case session.StatusStarting:
		return 4
	default:
		return 5
	}
}

// sortByUrgency keeps the sessions that need attention at the top of the list.
func (m *home) sortByUrgency() {
	if m.state != stateDefault || m.instanceStarting {
		return
	}
	items := m.list.GetInstances()
	sorted := make([]*session.Instance, len(items))
	copy(sorted, items)
	sort.SliceStable(sorted, func(a, b int) bool {
		ra, rb := urgencyRank(sorted[a]), urgencyRank(sorted[b])
		if ra != rb {
			return ra < rb
		}
		return sorted[a].Number < sorted[b].Number
	})
	m.list.Reorder(sorted)
}

// autoPauseIdle puts sessions that have waited for the user too long to sleep,
// stopping their agent process to free memory.
func (m *home) autoPauseIdle() bool {
	limit := m.appConfig.AutoPauseAfter()
	if limit <= 0 {
		return false
	}
	changed := false
	for _, inst := range m.list.GetInstances() {
		if inst.Status != session.Ready || inst.NeedsInput || inst.Menu != nil || inst.IdleSince.IsZero() || inst.WaitReason == "фоновая задача" ||
			inst == m.attached || inst.Conductor || inst.Assistant {
			continue
		}
		if time.Since(inst.IdleSince) < limit {
			continue
		}
		if inst.ClaudeConversationID() == "" {
			// Without the exact conversation id waking up could open the wrong one.
			continue
		}
		if err := inst.Hibernate(); err != nil {
			log.WarningLog.Printf("auto-pause %q: %v", inst.Title, err)
			inst.IdleSince = time.Now() // don't retry every tick
			continue
		}
		changed = true
	}
	return changed
}

// buildSummary renders where every task stands, for a status update.
func (m *home) buildSummary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Статус задач на %s\n", time.Now().Format("02.01 15:04"))
	for _, inst := range m.list.GetInstances() {
		label, _ := inst.StatusLabel()
		if stage := inst.Stage(); stage != "" {
			label += " · " + stage
		} else if stage := session.ClassifyRecap(inst.Recap); stage != "" && inst.Status != session.Running {
			label += " · " + stage
		}
		fmt.Fprintf(&b, "\n#%d %s — %s\n", inst.Number, inst.Title, label)
		if inst.Branch != "" {
			fmt.Fprintf(&b, "   ветка: %s\n", inst.Branch)
		}
		if inst.Note != "" {
			fmt.Fprintf(&b, "   %s\n", inst.Note)
		}
		if inst.Recap != "" {
			fmt.Fprintf(&b, "   %s\n", inst.Recap)
		}
	}
	return b.String()
}

// openReply shows a one-line input whose text is sent to target without attaching.
func (m *home) openReply(target *session.Instance) tea.Cmd {
	if target == nil || !target.Started() || target.Status == session.Loading {
		return nil
	}
	if target.Paused() {
		return m.handleError(fmt.Errorf("сессия «%s» на паузе: сначала запусти её клавишей r", target.Title))
	}
	m.replyTarget = target
	m.state = statePrompt
	m.menu.SetState(ui.StatePrompt)
	hint := "Enter — отправить · Tab — подсказка Claude · Esc — отмена"
	if target.Draft != "" {
		hint = "твой черновик подставлен, правь · Enter — отправить · Esc — оставить как было"
	}
	initial := target.Draft
	if initial == "" {
		initial = m.replyPrefill
	}
	m.replyPrefill = ""
	m.textInputOverlay = overlay.NewTextInputOverlay(
		fmt.Sprintf("Сообщение → %s   (%s)", target.Title, hint), initial)
	m.textInputOverlay.SetSubmitOnEnter(true)
	maxCtx := m.winH/2 - 4
	if maxCtx < 6 {
		maxCtx = 6
	}
	var ctx []overlay.ContextLine
	exchange, raw := target.RecentExchange(2, 120)
	for _, l := range exchange {
		ctx = append(ctx, overlay.ContextLine{Text: l.Text, User: l.User})
	}
	// Keep the last capture locally to diagnose what the agent's screen looks like.
	if dir, err := config.GetConfigDir(); err == nil && raw != "" {
		tail := raw
		if lines := strings.Split(raw, "\n"); len(lines) > 400 {
			tail = strings.Join(lines[len(lines)-400:], "\n")
		}
		_ = os.WriteFile(filepath.Join(dir, "last-reply-capture.txt"),
			[]byte(fmt.Sprintf("session: %s\nparsed lines: %d\n----\n%s", target.Title, len(exchange), tail)), 0600)
	}
	if len(ctx) == 0 {
		ctx = []overlay.ContextLine{{Text: "(не удалось прочитать переписку с экрана сессии)"}}
	}
	m.textInputOverlay.SetContext(ctx, maxCtx)
	if target.Draft == "" {
		m.textInputOverlay.SetSuggestion(target.Suggestion)
	}
	return tea.WindowSize()
}

// handleOverviewKey handles keys while the all-sessions overview is shown.
func (m *home) handleOverviewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	instances := m.list.GetInstances()
	n := len(instances)
	layout := ui.ComputeOverviewLayout(instances, m.winW)
	clamp := func() {
		if m.overviewIdx >= n {
			m.overviewIdx = n - 1
		}
		if m.overviewIdx < 0 {
			m.overviewIdx = 0
		}
	}
	switch msg.String() {
	case "v", "esc", "q":
		m.overview = false
		return m, tea.Batch(tea.WindowSize(), m.instanceChanged())
	case "left", "h":
		m.overviewIdx = layout.Move(m.overviewIdx, "left")
	case "right", "l":
		m.overviewIdx = layout.Move(m.overviewIdx, "right")
	case "up", "k":
		m.overviewIdx = layout.Move(m.overviewIdx, "up")
	case "down", "j":
		m.overviewIdx = layout.Move(m.overviewIdx, "down")
	case "enter", "o":
		if n == 0 {
			return m, nil
		}
		clamp()
		m.list.SetSelectedInstance(m.overviewIdx)
		m.overview = false
		m.instanceChanged()
		return m.handleKeyPress(msg)
	case "i":
		if n == 0 {
			return m, nil
		}
		clamp()
		m.list.SetSelectedInstance(m.overviewIdx)
		return m, m.openReply(instances[m.overviewIdx])
	case "p":
		m.appConfig.Pet = ui.NextPet(m.appConfig.Pet, m.appConfig.PetDir != "")
		if err := config.SaveConfig(m.appConfig); err != nil {
			log.WarningLog.Printf("save pet: %v", err)
		}
		return m, nil
	case "r", "C", "e", "D", "R":
		if n == 0 {
			return m, nil
		}
		clamp()
		m.list.SetSelectedInstance(m.overviewIdx)
		m.overview = false
		mod, cmd := m.handleKeyPress(msg)
		m.overview = true
		return mod, cmd
	}
	clamp()
	return m, nil
}

func (m *home) handleKeyPress(msg tea.KeyMsg) (mod tea.Model, cmd tea.Cmd) {
	// Shortcuts work on the Russian layout too: й → q, о → j, м → v… (not while typing text).
	if m.state == stateDefault || m.state == stateHelp {
		msg = latinKey(msg)
	}
	if m.queue && m.state == stateDefault {
		return m.handleQueueKey(msg)
	}
	if m.overview && m.state == stateDefault {
		return m.handleOverviewKey(msg)
	}
	cmd, returnEarly := m.handleMenuHighlighting(msg)
	if returnEarly {
		return m, cmd
	}

	if m.state == stateHelp {
		return m.handleHelpState(msg)
	}

	if m.state == stateNew {
		// Handle quit commands first. Don't handle q because the user might want to type that.
		if msg.String() == "ctrl+c" {
			m.state = stateDefault
			m.promptAfterName = false
			m.list.Kill()
			return m, tea.Sequence(
				tea.WindowSize(),
				func() tea.Msg {
					m.mu.Lock()
					defer m.mu.Unlock()
					m.menu.SetState(ui.StateDefault)
					return nil
				},
			)
		}

		instance := m.list.GetInstances()[m.list.NumInstances()-1]
		switch msg.Type {
		// Start the instance (enable previews etc) and go back to the main menu state.
		case tea.KeyEnter:
			if len(instance.Title) == 0 {
				return m, m.handleError(fmt.Errorf("title cannot be empty"))
			}

			// If promptAfterName, show prompt+branch overlay before starting
			if m.promptAfterName {
				m.promptAfterName = false
				m.state = statePrompt
				m.menu.SetState(ui.StatePrompt)
				m.textInputOverlay = m.newPromptOverlay()
				// Trigger initial branch search (no debounce, version 0)
				initialSearch := m.runBranchSearch("", m.textInputOverlay.BranchFilterVersion())
				return m, tea.Batch(tea.WindowSize(), initialSearch)
			}

			// Set Loading status and finalize into the list immediately
			instance.SetStatus(session.Loading)
			m.newInstanceFinalizer()
			m.promptAfterName = false
			m.state = stateDefault
			m.menu.SetState(ui.StateDefault)

			// Return a tea.Cmd that runs instance.Start in the background
			startCmd := func() tea.Msg {
				err := instance.Start(true)
				return instanceStartedMsg{
					instance:        instance,
					err:             err,
					promptAfterName: false,
				}
			}

			return m, tea.Batch(tea.WindowSize(), m.instanceChanged(), startCmd)
		case tea.KeyRunes:
			if runewidth.StringWidth(instance.Title) >= 32 {
				return m, m.handleError(fmt.Errorf("title cannot be longer than 32 characters"))
			}
			if err := instance.SetTitle(instance.Title + string(msg.Runes)); err != nil {
				return m, m.handleError(err)
			}
		case tea.KeyBackspace:
			runes := []rune(instance.Title)
			if len(runes) == 0 {
				return m, nil
			}
			if err := instance.SetTitle(string(runes[:len(runes)-1])); err != nil {
				return m, m.handleError(err)
			}
		case tea.KeySpace:
			if err := instance.SetTitle(instance.Title + " "); err != nil {
				return m, m.handleError(err)
			}
		case tea.KeyEsc:
			m.list.Kill()
			m.state = stateDefault
			m.instanceChanged()

			return m, tea.Sequence(
				tea.WindowSize(),
				func() tea.Msg {
					m.mu.Lock()
					defer m.mu.Unlock()
					m.menu.SetState(ui.StateDefault)
					return nil
				},
			)
		default:
		}
		return m, nil
	} else if m.state == statePrompt {
		// Handle cancel via ctrl+c before delegating to the overlay
		if msg.String() == "ctrl+c" {
			return m, m.cancelPromptOverlay()
		}

		// Use the new TextInputOverlay component to handle all key events
		shouldClose, branchFilterChanged := m.textInputOverlay.HandleKeyPress(msg)

		// Check if the form was submitted or canceled
		if shouldClose && m.replyTarget != nil {
			target := m.replyTarget
			m.replyTarget = nil
			var sendErr error
			if m.textInputOverlay.AcceptRemote {
				sendErr = target.AcceptSuggestion()
			} else if m.textInputOverlay.IsSubmitted() {
				text := strings.TrimSpace(strings.ReplaceAll(m.textInputOverlay.GetValue(), "\n", " "))
				if text != "" {
					sendErr = target.ReplaceInput(text)
				}
			}
			m.textInputOverlay = nil
			m.state = stateDefault
			cmds := []tea.Cmd{tea.WindowSize(), func() tea.Msg {
				m.mu.Lock()
				defer m.mu.Unlock()
				m.menu.SetState(ui.StateDefault)
				return nil
			}}
			if sendErr != nil {
				cmds = append(cmds, m.handleError(sendErr))
			}
			return m, tea.Sequence(cmds...)
		}
		if shouldClose {
			selected := m.list.GetSelectedInstance()
			if selected == nil {
				return m, nil
			}

			if m.textInputOverlay.IsCanceled() {
				m.editingNote = false
				return m, m.cancelPromptOverlay()
			}

			if m.renaming {
				m.renaming = false
				var renameErr error
				if m.textInputOverlay.IsSubmitted() {
					newTitle := strings.TrimSpace(strings.ReplaceAll(m.textInputOverlay.GetValue(), "\n", " "))
					for _, other := range m.list.GetInstances() {
						if other != selected && (other.Title == newTitle || tmux.TmuxNameFor(other.Title) == tmux.TmuxNameFor(newTitle)) {
							renameErr = fmt.Errorf("сессия «%s» уже есть (или слишком похожее имя)", newTitle)
						}
					}
					if renameErr == nil && runewidth.StringWidth(newTitle) > 32 {
						renameErr = fmt.Errorf("название длиннее 32 символов")
					}
					if renameErr == nil && newTitle != selected.Title {
						renameErr = selected.Rename(newTitle)
						if renameErr == nil {
							renameErr = m.storage.SaveInstances(m.list.GetInstances())
						}
					}
				}
				m.textInputOverlay = nil
				m.state = stateDefault
				cmds := []tea.Cmd{tea.WindowSize(), func() tea.Msg {
					m.mu.Lock()
					defer m.mu.Unlock()
					m.menu.SetState(ui.StateDefault)
					return nil
				}}
				if renameErr != nil {
					cmds = append(cmds, m.handleError(renameErr))
				}
				return m, tea.Sequence(cmds...)
			}
			if m.editingNote {
				m.editingNote = false
				if m.textInputOverlay.IsSubmitted() {
					selected.Note = strings.Join(strings.Fields(m.textInputOverlay.GetValue()), " ")
					if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
						log.ErrorLog.Printf("failed to save note: %v", err)
					}
				}
				m.textInputOverlay = nil
				m.state = stateDefault
				return m, tea.Sequence(
					tea.WindowSize(),
					func() tea.Msg {
						m.mu.Lock()
						defer m.mu.Unlock()
						m.menu.SetState(ui.StateDefault)
						return nil
					},
				)
			}

			if m.textInputOverlay.IsSubmitted() {
				prompt := m.textInputOverlay.GetValue()
				selectedBranch := m.textInputOverlay.GetSelectedBranch()
				selectedProgram := m.textInputOverlay.GetSelectedProgram()

				if !selected.Started() {
					// Shift+N flow: instance not started yet — set branch, start, then send prompt
					if selectedBranch != "" {
						selected.SetSelectedBranch(selectedBranch)
					}
					if selectedProgram != "" {
						selected.Program = selectedProgram
					}
					selected.Prompt = prompt

					// Finalize into list and start
					selected.SetStatus(session.Loading)
					m.newInstanceFinalizer()
					m.textInputOverlay = nil
					m.state = stateDefault
					m.menu.SetState(ui.StateDefault)

					startCmd := func() tea.Msg {
						err := selected.Start(true)
						return instanceStartedMsg{
							instance:        selected,
							err:             err,
							promptAfterName: false,
							selectedBranch:  selectedBranch,
						}
					}

					return m, tea.Batch(tea.WindowSize(), m.instanceChanged(), startCmd)
				}

				// Regular flow: instance already running, just send prompt
				if err := selected.SendPrompt(prompt); err != nil {
					return m, m.handleError(err)
				}
			}

			// Close the overlay and reset state
			m.textInputOverlay = nil
			m.state = stateDefault
			return m, tea.Sequence(
				tea.WindowSize(),
				func() tea.Msg {
					m.mu.Lock()
					defer m.mu.Unlock()
					m.menu.SetState(ui.StateDefault)
					m.showHelpScreen(helpStart(selected), nil)
					return nil
				},
			)
		}

		// Schedule a debounced branch search if the filter changed
		if branchFilterChanged {
			filter := m.textInputOverlay.BranchFilter()
			version := m.textInputOverlay.BranchFilterVersion()
			return m, m.scheduleBranchSearch(filter, version)
		}

		return m, nil
	}

	// Handle confirmation state
	if m.state == stateConfirm {
		shouldClose := m.confirmationOverlay.HandleKeyPress(msg)
		if shouldClose {
			m.state = stateDefault
			m.confirmationOverlay = nil
			return m, nil
		}
		return m, nil
	}

	// Exit scrolling mode when ESC is pressed and preview pane is in scrolling mode
	// Check if Escape key was pressed and we're not in the diff tab (meaning we're in preview tab)
	// Always check for escape key first to ensure it doesn't get intercepted elsewhere
	if msg.Type == tea.KeyEsc {
		// If in preview tab and in scroll mode, exit scroll mode
		if m.tabbedWindow.IsInPreviewTab() && m.tabbedWindow.IsPreviewInScrollMode() {
			// Use the selected instance from the list
			selected := m.list.GetSelectedInstance()
			err := m.tabbedWindow.ResetPreviewToNormalMode(selected)
			if err != nil {
				return m, m.handleError(err)
			}
			return m, m.instanceChanged()
		}
		// If in terminal tab and in scroll mode, exit scroll mode
		if m.tabbedWindow.IsInTerminalTab() && m.tabbedWindow.IsTerminalInScrollMode() {
			m.tabbedWindow.ResetTerminalToNormalMode()
			return m, m.instanceChanged()
		}
	}

	// Handle quit commands first
	if msg.String() == "ctrl+c" || msg.String() == "q" {
		return m.handleQuit()
	}

	name, ok := keys.GlobalKeyStringsMap[msg.String()]
	if !ok {
		return m, nil
	}

	switch name {
	case keys.KeyHelp:
		return m.showHelpScreen(helpTypeGeneral{}, nil)
	case keys.KeyPrompt:
		if m.list.NumInstances() >= GlobalInstanceLimit {
			return m, m.handleError(
				fmt.Errorf("you can't create more than %d instances", GlobalInstanceLimit))
		}

		// Start a background fetch so branches are up to date by the time the picker opens
		fetchCmd := func() tea.Msg {
			currentDir, _ := os.Getwd()
			git.FetchBranches(currentDir)
			return nil
		}

		instance, err := session.NewInstance(session.InstanceOptions{
			Title:   "",
			Path:    ".",
			Program: m.program,
		})
		if err != nil {
			return m, m.handleError(err)
		}

		m.newInstanceFinalizer = m.list.AddInstance(instance)
		m.list.SetSelectedInstance(m.list.NumInstances() - 1)
		m.state = stateNew
		m.menu.SetState(ui.StateNewInstance)
		m.promptAfterName = true

		return m, fetchCmd
	case keys.KeyNew:
		if m.list.NumInstances() >= GlobalInstanceLimit {
			return m, m.handleError(
				fmt.Errorf("you can't create more than %d instances", GlobalInstanceLimit))
		}
		instance, err := session.NewInstance(session.InstanceOptions{
			Title:   "",
			Path:    ".",
			Program: m.program,
		})
		if err != nil {
			return m, m.handleError(err)
		}

		m.newInstanceFinalizer = m.list.AddInstance(instance)
		m.list.SetSelectedInstance(m.list.NumInstances() - 1)
		m.state = stateNew
		m.menu.SetState(ui.StateNewInstance)

		return m, nil
	case keys.KeyUp:
		m.list.Up()
		return m, m.instanceChanged()
	case keys.KeyDown:
		m.list.Down()
		return m, m.instanceChanged()
	case keys.KeyShiftUp:
		m.tabbedWindow.ScrollUp()
		return m, m.instanceChanged()
	case keys.KeyShiftDown:
		m.tabbedWindow.ScrollDown()
		return m, m.instanceChanged()
	case keys.KeyTab:
		m.tabbedWindow.Toggle()
		m.menu.SetActiveTab(m.tabbedWindow.GetActiveTab())
		return m, m.instanceChanged()
	case keys.KeyKill:
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Status == session.Loading {
			return m, nil
		}

		// Create the kill action as a tea.Cmd
		killAction := func() tea.Msg {
			// Get worktree and check if branch is checked out
			worktree, err := selected.GetGitWorktree()
			if err != nil {
				return err
			}

			checkedOut, err := worktree.IsBranchCheckedOut()
			if err != nil {
				return err
			}

			if checkedOut {
				return fmt.Errorf("instance %s is currently checked out", selected.Title)
			}

			// Clean up terminal session for this instance
			m.tabbedWindow.CleanupTerminalForInstance(selected.Title)

			// Delete from storage first
			if err := m.storage.DeleteInstance(selected.Title); err != nil {
				return err
			}

			// Then kill the instance
			m.list.Kill()
			return instanceChangedMsg{}
		}

		// Show confirmation modal
		message := fmt.Sprintf("[!] Kill session '%s'?", selected.Title)
		return m, m.confirmAction(message, killAction)
	case keys.KeySubmit:
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Status == session.Loading {
			return m, nil
		}

		// Create the push action as a tea.Cmd
		pushAction := func() tea.Msg {
			// Default commit message with timestamp
			commitMsg := fmt.Sprintf("[claudesquad] update from '%s' on %s", selected.Title, time.Now().Format(time.RFC822))
			worktree, err := selected.GetGitWorktree()
			if err != nil {
				return err
			}
			if err = worktree.PushChanges(commitMsg, true); err != nil {
				return err
			}
			return nil
		}

		// Show confirmation modal
		message := fmt.Sprintf("[!] Push changes from session '%s'?", selected.Title)
		return m, m.confirmAction(message, pushAction)
	case keys.KeyCheckout:
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Status == session.Loading {
			return m, nil
		}

		// Show help screen before pausing
		m.showHelpScreen(helpTypeInstanceCheckout{}, func() {
			if err := selected.Pause(); err != nil {
				m.handleError(err)
			}
			m.tabbedWindow.CleanupTerminalForInstance(selected.Title)
			m.instanceChanged()
		})
		return m, nil
	case keys.KeyMoveUp:
		if m.list.MoveUp() {
			if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
				return m, m.handleError(err)
			}
			return m, m.instanceChanged()
		}
		return m, nil
	case keys.KeyMoveDown:
		if m.list.MoveDown() {
			if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
				return m, m.handleError(err)
			}
			return m, m.instanceChanged()
		}
		return m, nil
	case keys.KeyReply:
		return m, m.openReply(m.list.GetSelectedInstance())
	case keys.KeyWhatHappened:
		since := time.Now().Add(-16 * time.Hour)
		if data, err := os.ReadFile(reportStampPath()); err == nil {
			if t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data))); err == nil && time.Since(t) > time.Hour {
				since = t
			}
		}
		text := m.buildReport(since)
		if text == "" {
			text = "С " + since.Format("02.01 15:04") + " ничего заметного не произошло.\n\n(любая клавиша — закрыть)"
		}
		m.showReport(text)
		return m, nil
	case keys.KeyOpenReport:
		selected := m.list.GetSelectedInstance()
		if selected == nil {
			return m, nil
		}
		p := qaReport(selected)
		if p == "" {
			return m, m.handleError(fmt.Errorf("у «%s» нет отчёта report.html", selected.Title))
		}
		if err := openPath(p); err != nil {
			return m, m.handleError(err)
		}
		return m, nil
	case keys.KeyOpenFolder:
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.GetWorktreePath() == "" {
			return m, nil
		}
		if err := openPath(selected.GetWorktreePath()); err != nil {
			return m, m.handleError(err)
		}
		return m, nil
	case keys.KeySummary:
		summary := m.buildSummary()
		copied := "Скопировано в буфер обмена — вставляй куда нужно."
		if err := clipboard.WriteAll(summary); err != nil {
			copied = "Не удалось скопировать в буфер: " + err.Error()
		}
		m.textOverlay = overlay.NewTextOverlay(summary + "\n" + copied + "\n\n(любая клавиша — закрыть)")
		m.textOverlay.SetWidth(int(float32(m.winW) * 0.8))
		m.state = stateHelp
		return m, nil
	case keys.KeyRename:
		selected := m.list.GetSelectedInstance()
		if selected == nil || !selected.Started() || selected.Status == session.Loading {
			return m, nil
		}
		m.renaming = true
		m.state = statePrompt
		m.menu.SetState(ui.StatePrompt)
		m.textInputOverlay = overlay.NewTextInputOverlay("Новое название сессии (Enter — сохранить, Esc — отмена)", selected.Title)
		m.textInputOverlay.SetSubmitOnEnter(true)
		return m, tea.WindowSize()
	case keys.KeyQueue:
		m.queue, m.queueIdx = true, 0
		return m, tea.WindowSize()
	case keys.KeyOverview:
		m.overview = true
		for idx, inst := range m.list.GetInstances() {
			if inst == m.list.GetSelectedInstance() {
				m.overviewIdx = idx
			}
		}
		return m, tea.WindowSize()
	case keys.KeyColor:
		selected := m.list.GetSelectedInstance()
		if selected == nil || !selected.Started() {
			return m, nil
		}
		selected.CycleColor()
		if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
			return m, m.handleError(err)
		}
		return m, m.instanceChanged()
	case keys.KeyNote:
		selected := m.list.GetSelectedInstance()
		if selected == nil || !selected.Started() {
			return m, nil
		}
		m.editingNote = true
		m.state = statePrompt
		m.menu.SetState(ui.StatePrompt)
		m.textInputOverlay = overlay.NewTextInputOverlay("Описание сессии (что в ней)", selected.Note)
		return m, tea.WindowSize()
	case keys.KeyResume:
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Status == session.Loading {
			return m, nil
		}
		if err := selected.Resume(); err != nil {
			return m, m.handleError(err)
		}
		return m, tea.WindowSize()
	case keys.KeyEnter:
		if m.list.NumInstances() == 0 {
			return m, nil
		}
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Paused() || selected.Status == session.Loading || !selected.TmuxAlive() {
			return m, nil
		}
		// Terminal tab: attach to terminal session
		if m.tabbedWindow.IsInTerminalTab() {
			m.showHelpScreen(helpTypeInstanceAttach{}, func() {
				ch, err := m.tabbedWindow.AttachTerminal()
				if err != nil {
					m.handleError(err)
					return
				}
				m.waitDetached(ch, "пользователь в терминале сессии «"+selected.Title+"»", false, m.tabbedWindow.ForceDetachTerminal)
				m.state = stateDefault
			})
			return m, nil
		}
		// Show help screen before attaching
		m.showHelpScreen(helpTypeInstanceAttach{}, func() {
			ch, err := m.list.Attach()
			if err != nil {
				m.handleError(err)
				return
			}
			m.waitDetached(ch, "пользователь внутри сессии «"+selected.Title+"»", true, selected.ForceDetach)
			m.state = stateDefault
			m.instanceChanged()
		})
		return m, nil
	default:
		return m, nil
	}
}

// instanceChanged updates the preview pane, menu, and diff pane based on the selected instance. It returns an error
// Cmd if there was any error.
func (m *home) instanceChanged() tea.Cmd {
	// selected may be nil
	selected := m.list.GetSelectedInstance()

	m.tabbedWindow.UpdateDiff(selected)
	m.tabbedWindow.SetInstance(selected)
	// Update menu with current instance
	m.menu.SetInstance(selected)

	// If there's no selected instance, we don't need to update the preview.
	if err := m.tabbedWindow.UpdatePreview(selected); err != nil {
		return m.handleError(err)
	}
	if err := m.tabbedWindow.UpdateTerminal(selected); err != nil {
		return m.handleError(err)
	}
	return nil
}

type keyupMsg struct{}

// keydownCallback clears the menu option highlighting after 500ms.
func (m *home) keydownCallback(name keys.KeyName) tea.Cmd {
	m.menu.Keydown(name)
	return func() tea.Msg {
		select {
		case <-m.ctx.Done():
		case <-time.After(500 * time.Millisecond):
		}

		return keyupMsg{}
	}
}

// hideErrMsg implements tea.Msg and clears the error text from the screen.
type hideErrMsg struct{}

// previewTickMsg implements tea.Msg and triggers a preview update
type previewTickMsg struct{}

type instanceChangedMsg struct{}

type instanceStartedMsg struct {
	instance        *session.Instance
	err             error
	promptAfterName bool
	selectedBranch  string
}

// branchSearchDebounceMsg fires after the debounce interval to trigger a search.
type branchSearchDebounceMsg struct {
	filter  string
	version uint64
}

// branchSearchResultMsg carries search results back to Update.
type branchSearchResultMsg struct {
	branches []string
	version  uint64
}

const branchSearchDebounce = 150 * time.Millisecond

// scheduleBranchSearch returns a debounced tea.Cmd: sleeps, then triggers a search message.
func (m *home) scheduleBranchSearch(filter string, version uint64) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(branchSearchDebounce)
		return branchSearchDebounceMsg{filter: filter, version: version}
	}
}

// runBranchSearch returns a tea.Cmd that performs the git search in the background.
func (m *home) runBranchSearch(filter string, version uint64) tea.Cmd {
	return func() tea.Msg {
		currentDir, _ := os.Getwd()
		branches, err := git.SearchBranches(currentDir, filter)
		if err != nil {
			log.WarningLog.Printf("branch search failed: %v", err)
			return nil
		}
		return branchSearchResultMsg{branches: branches, version: version}
	}
}

// instanceMetaResult holds the results of a single instance's metadata update,
// computed in a background goroutine.
type instanceMetaResult struct {
	instance  *session.Instance
	updated   bool
	hasPrompt bool
	dead      bool // the tmux session is gone
	content   string
	diffStats *git.DiffStats
}

// metadataUpdateDoneMsg is sent when the background metadata update completes.
type metadataUpdateDoneMsg struct {
	results []instanceMetaResult
}

// instanceStartDoneMsg is sent when the background instance start completes.
type instanceStartDoneMsg struct {
	instance *session.Instance
	err      error
}

// runInstanceStartCmd returns a Cmd that performs the expensive instance.Start(true)
// in a background goroutine so the main event loop stays responsive.
func runInstanceStartCmd(instance *session.Instance) tea.Cmd {
	return func() tea.Msg {
		err := instance.Start(true)
		return instanceStartDoneMsg{instance: instance, err: err}
	}
}

// snapshotActiveInstances returns the currently active (started, not paused)
// instances. Called on the main thread so the filtering doesn't race with
// state mutations.
func (m *home) snapshotActiveInstances() []*session.Instance {
	var out []*session.Instance
	for _, inst := range m.list.GetInstances() {
		if inst.Started() && !inst.Paused() {
			out = append(out, inst)
		}
	}
	return out
}

// tickUpdateMetadataCmd returns a self-chaining Cmd that sleeps 500ms, then performs
// expensive metadata I/O (tmux capture, git diff) in parallel background goroutines.
// Because it only re-schedules after completing, overlapping ticks are impossible.
// The active instances slice should be snapshotted on the main thread via
// snapshotActiveInstances() before being passed here.
//
// Only the selected instance gets a full diff (with Content); the rest get a
// lightweight numstat-only summary. This keeps per-instance memory bounded
// since the diff pane only ever renders the selected one.
func tickUpdateMetadataCmd(active []*session.Instance, selected *session.Instance) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(time.Second)
		return metadataUpdateDoneMsg{results: collectMetadata(active, selected)}
	}
}

// collectMetadata does the expensive per-session I/O (tmux capture, git diff) in
// parallel. It touches only what the monitor owns, so it runs without m.mu.
func collectMetadata(active []*session.Instance, selected *session.Instance) []instanceMetaResult {
	if len(active) == 0 {
		return nil
	}
	results := make([]instanceMetaResult, len(active))
	var wg sync.WaitGroup
	for idx, inst := range active {
		wg.Add(1)
		go func(i int, instance *session.Instance) {
			defer wg.Done()
			r := &results[i]
			r.instance = instance
			r.updated, r.hasPrompt, r.content = instance.HasUpdatedWithContent()
			if r.content == "" && !instance.TmuxAlive() {
				r.dead = true
				return
			}
			// git diff on a big repo is expensive: refresh the selected session's
			// diff every few seconds and the others' rarely. nil keeps the old stats.
			since := time.Since(instance.LastDiffAt)
			if instance == selected && since > 3*time.Second {
				r.diffStats = instance.ComputeDiff()
				instance.LastDiffAt = time.Now()
			} else if since > 45*time.Second {
				r.diffStats = instance.ComputeDiffNumstat()
				instance.LastDiffAt = time.Now()
			}
		}(idx, inst)
	}
	wg.Wait()

	return results
}

// processMetadata applies one round of session monitoring: statuses, recaps, sleep,
// requests from cs ctl, the status snapshot and conductor digests. It runs from the
// UI loop and, while the UI is blocked (a session is open full screen), from the
// background loop, always with m.mu held.
func (m *home) processMetadata(results []instanceMetaResult) {
	recapChanged := false
	for _, r := range results {
		// Skip instances that were paused while metadata was being computed
		if r.instance.Status == session.Paused {
			continue
		}
		if r.dead && r.instance != m.attached {
			// The agent process is gone (closed, crashed, tmux restarted): show the
			// session as asleep so r starts it again with the same conversation.
			r.instance.ReleaseDeadSession()
			r.instance.Hibernated = true
			r.instance.SetStatus(session.Paused)
			recapChanged = true
			continue
		}
		if r.dead {
			continue
		}
		if r.content != "" && r.instance.ApplyPaneUpdate(r.content, r.hasPrompt, r.updated) {
			recapChanged = true
		}
		if r.instance == m.attached {
			r.instance.CallsUserSeen = r.instance.CallsUser // the user is looking at it right now
		}
		if r.instance != m.attached && r.instance.AcceptResumeChoice(r.content) {
			log.InfoLog.Printf("%q: accepted the resume question with its default", r.instance.Title)
		}
		if r.updated {
			r.instance.SetStatus(session.Running)
			r.instance.IdleSince = time.Now()
			// the screen also changes by itself (status line, pets): count only real work,
			// when Claude shows its spinner ("✶ Cultivating… (1m 40s", "esc to interrupt")
			if workingRe.MatchString(r.content) {
				r.instance.ActiveAt = r.instance.IdleSince
			}
		} else if r.hasPrompt {
			r.instance.TapEnter()
		} else {
			r.instance.SetStatus(session.Ready)
		}
		if r.diffStats == nil {
			// Not refreshed this tick: keep the previous stats.
		} else if r.diffStats.Error != nil {
			if msg := r.diffStats.Error.Error(); !strings.Contains(msg, "base commit SHA not set") && m.diffErrLogged[r.instance.Title] != msg {
				// Once per session and error: a vanished worktree used to repeat this every second.
				if m.diffErrLogged == nil {
					m.diffErrLogged = map[string]string{}
				}
				m.diffErrLogged[r.instance.Title] = msg
				log.WarningLog.Printf("could not update diff stats for %q: %v", r.instance.Title, r.diffStats.Error)
			}
			r.instance.SetDiffStats(nil)
		} else {
			r.instance.SetDiffStats(r.diffStats)
		}
	}
	for _, inst := range m.list.GetInstances() {
		m.deliverPendingPrompt(inst)
	}
	m.runDailyJobs()
	if m.takeControlRequests() {
		recapChanged = true
	}
	// New/kill requests change the list: only when no window or input is open,
	// so they never mix with what the user is doing. They wait in the queue.
	// A text window (help, «что было»), the overview and the queue do not depend on
	// which session is selected; only prompts, confirmations and naming do.
	if (m.state == stateDefault || m.state == stateHelp) && !m.instanceStarting {
		if m.takeSessionRequests() {
			recapChanged = true
		}
	}
	m.handleNetwork()
	m.handleUsage()
	m.refreshDensity()
	m.updateConductorActive()
	m.refreshWipes()
	m.handleLimits(time.Now())
	m.watchConductor(time.Now())
	m.handleMenus(time.Now())
	m.recordStatuses()
	session.SnapshotExtra.VPNDown = m.vpnDown()
	m.maybeWakeConductor()
	if m.autoPauseIdle() {
		recapChanged = true
	}
	if m.assignNumbers() {
		recapChanged = true
	}
	m.sortByUrgency()
	if time.Since(m.lastSnapshotAt) > 5*time.Second {
		if err := session.WriteStatusSnapshot(m.list.GetInstances(), m.uiWaitingDesc()); err != nil {
			log.WarningLog.Printf("status snapshot: %v", err)
		}
		m.lastSnapshotAt = time.Now()
	}
	if recapChanged {
		// Keep the latest recap so paused sessions still show where they stand.
		if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
			log.WarningLog.Printf("could not save recap: %v", err)
		}
	}
}

// handleError handles all errors which get bubbled up to the app. sets the error message. We return a callback tea.Cmd that returns a hideErrMsg message
// which clears the error message after 3 seconds.
func (m *home) handleError(err error) tea.Cmd {
	log.ErrorLog.Printf("%v", err)
	m.errBox.SetError(err)
	return func() tea.Msg {
		select {
		case <-m.ctx.Done():
		case <-time.After(3 * time.Second):
		}

		return hideErrMsg{}
	}
}

func (m *home) newPromptOverlay() *overlay.TextInputOverlay {
	return overlay.NewTextInputOverlayWithBranchPicker("Enter prompt", "", m.appConfig.GetProfiles())
}

// cancelPromptOverlay cancels the prompt overlay, cleaning up unstarted instances.
func (m *home) cancelPromptOverlay() tea.Cmd {
	m.replyTarget = nil
	m.editingNote = false
	m.renaming = false
	selected := m.list.GetSelectedInstance()
	if selected != nil && !selected.Started() {
		m.list.Kill()
	}
	m.textInputOverlay = nil
	m.state = stateDefault
	return tea.Sequence(
		tea.WindowSize(),
		func() tea.Msg {
			m.mu.Lock()
			defer m.mu.Unlock()
			m.menu.SetState(ui.StateDefault)
			return nil
		},
	)
}

// confirmAction shows a confirmation modal and stores the action to execute on confirm
func (m *home) confirmAction(message string, action tea.Cmd) tea.Cmd {
	m.state = stateConfirm

	// Create and show the confirmation overlay using ConfirmationOverlay
	m.confirmationOverlay = overlay.NewConfirmationOverlay(message)
	// Set a fixed width for consistent appearance
	m.confirmationOverlay.SetWidth(50)

	// Set callbacks for confirmation and cancellation
	m.confirmationOverlay.OnConfirm = func() {
		m.state = stateDefault
		// Execute the action if it exists
		if action != nil {
			_ = action()
		}
	}

	m.confirmationOverlay.OnCancel = func() {
		m.state = stateDefault
	}

	return nil
}

func (m *home) View() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.view()
}

func (m *home) view() string {
	listWithPadding := lipgloss.NewStyle().PaddingTop(1).Render(m.list.String())
	previewWithPadding := lipgloss.NewStyle().PaddingTop(1).Render(m.tabbedWindow.String())
	listAndPreview := lipgloss.JoinHorizontal(lipgloss.Top, listWithPadding, previewWithPadding)

	mainView := lipgloss.JoinVertical(
		lipgloss.Center,
		listAndPreview,
		m.menu.String(),
		m.errBox.String(),
	)
	if m.queue {
		mainView = lipgloss.JoinVertical(
			lipgloss.Left,
			ui.RenderQueue(session.UserQueue(m.list.GetInstances()), m.queueIdx, m.winW, m.winH-1, time.Now()),
			m.errBox.String(),
		)
	} else if m.overview {
		mainView = lipgloss.JoinVertical(
			lipgloss.Left,
			m.petOverlay(ui.RenderOverview(m.list.GetInstances(), m.overviewIdx, m.winW, m.winH-1), m.winW, m.winH-1),
			m.errBox.String(),
		)
	}

	if m.state == statePrompt {
		if m.textInputOverlay == nil {
			log.ErrorLog.Printf("text input overlay is nil")
		}
		return overlay.PlaceOverlay(0, 0, m.textInputOverlay.Render(), mainView, true, true)
	} else if m.state == stateHelp {
		if m.textOverlay == nil {
			log.ErrorLog.Printf("text overlay is nil")
		}
		return overlay.PlaceOverlay(0, 0, m.textOverlay.Render(), mainView, true, true)
	} else if m.state == stateConfirm {
		if m.confirmationOverlay == nil {
			log.ErrorLog.Printf("confirmation overlay is nil")
		}
		return overlay.PlaceOverlay(0, 0, m.confirmationOverlay.Render(), mainView, true, true)
	}

	return mainView
}

// updateConductorActive tells the sessions whether a conductor takes care of
// finished workers ("ждёт дирижёра") and drops marks of agents working again.
func (m *home) updateConductorActive() {
	active := false
	for _, inst := range m.list.GetInstances() {
		inst.ClearMarkWhenWorking()
		if inst.Conductor && inst.Started() && m.appConfig.ConductorInterval() > 0 {
			active = true
		}
	}
	session.ConductorActive = active
	session.RemoteControlWorkers = m.appConfig.RemoteControl
}

// wipeShownFor: how long the row shows that a forgotten draft was erased.
const wipeShownFor = 10 * time.Minute

// refreshWipes shows on the session rows which forgotten drafts were erased
// (by Squad or cs ctl send), read from wiped-drafts.log every few seconds.
func (m *home) refreshWipes() {
	if time.Since(m.wipesReadAt) < 5*time.Second {
		return
	}
	m.wipesReadAt = time.Now()
	wipes := session.ReadRecentWipes(wipeShownFor)
	for _, inst := range m.list.GetInstances() {
		if w, ok := wipes[inst.Title]; ok {
			inst.WipedText, inst.WipedAt = w.Text, w.At
		} else {
			inst.WipedText = ""
		}
	}
}

// ruToLatin maps the Russian ЙЦУКЕН layout to the keys at the same places.
var ruToLatin = map[rune]rune{
	'й': 'q', 'ц': 'w', 'у': 'e', 'к': 'r', 'е': 't', 'н': 'y', 'г': 'u', 'ш': 'i', 'щ': 'o', 'з': 'p',
	'ф': 'a', 'ы': 's', 'в': 'd', 'а': 'f', 'п': 'g', 'р': 'h', 'о': 'j', 'л': 'k', 'д': 'l',
	'я': 'z', 'ч': 'x', 'с': 'c', 'м': 'v', 'и': 'b', 'т': 'n', 'ь': 'm',
	'Й': 'Q', 'Ц': 'W', 'У': 'E', 'К': 'R', 'Е': 'T', 'Н': 'Y', 'Г': 'U', 'Ш': 'I', 'Щ': 'O', 'З': 'P',
	'Ф': 'A', 'Ы': 'S', 'В': 'D', 'А': 'F', 'П': 'G', 'Р': 'H', 'О': 'J', 'Л': 'K', 'Д': 'L',
	'Я': 'Z', 'Ч': 'X', 'С': 'C', 'М': 'V', 'И': 'B', 'Т': 'N', 'Ь': 'M', ',': '?',
}

// latinKey turns a single Cyrillic key press into the Latin key at the same place.
func latinKey(msg tea.KeyMsg) tea.KeyMsg {
	if msg.Type != tea.KeyRunes || len(msg.Runes) != 1 {
		return msg
	}
	if r, ok := ruToLatin[msg.Runes[0]]; ok && msg.Runes[0] != ',' {
		msg.Runes = []rune{r}
	}
	return msg
}
