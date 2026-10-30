package main

import (
	"claude-squad/config"
	"claude-squad/session"
	"claude-squad/session/tmux"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// `cs ctl` lets another program (the conductor session) see and drive sessions.

var (
	ctlJSON      bool
	ctlTurns     int
	ctlLines     int
	ctlForce     bool
	ctlNewDir    string
	ctlNewTitle  string
	ctlNewBranch string
	ctlNewPrompt string
	ctlNewColor  string
	ctlNewNote   string
	ctlNewGroup  string
	ctlNewProg   string
	ctlNewCond   bool
	ctlNewAsst   bool
	ctlWait      int
)

func loadSnapshot() (*session.StatusSnapshot, error) {
	snap, err := session.ReadStatusSnapshot()
	if err != nil {
		return nil, err
	}
	if time.Since(snap.UpdatedAt) > 2*time.Minute {
		fmt.Fprintf(os.Stderr, "внимание: снимок статусов устарел (%s) — Squad не запущен или завис\n",
			snap.UpdatedAt.Format("15:04"))
	} else if snap.UIWaiting != "" {
		fmt.Fprintf(os.Stderr, "Squad работает; сейчас: %s\n", snap.UIWaiting)
	}
	return snap, nil
}

func findSession(snap *session.StatusSnapshot, ref string) (*session.StatusEntry, error) {
	ref = strings.TrimPrefix(ref, "#")
	if n, err := strconv.Atoi(ref); err == nil {
		for i := range snap.Sessions {
			if snap.Sessions[i].Number == n {
				return &snap.Sessions[i], nil
			}
		}
		return nil, fmt.Errorf("нет сессии #%d", n)
	}
	for i := range snap.Sessions {
		if strings.EqualFold(snap.Sessions[i].Title, ref) {
			return &snap.Sessions[i], nil
		}
	}
	return nil, fmt.Errorf("нет сессии «%s»", ref)
}

func tmuxName(e *session.StatusEntry) string {
	if e.TmuxName != "" {
		return e.TmuxName
	}
	return tmux.TmuxNameFor(e.Title)
}

func capture(name string, history bool) (string, error) {
	args := []string{"capture-pane", "-p", "-e", "-J", "-t", "=" + name + ":"}
	if history {
		args = append(args, "-S", "-3000")
	}
	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		return "", fmt.Errorf("сессия не запущена или недоступна (%v)", err)
	}
	return string(out), nil
}

var ctlCmd = &cobra.Command{
	Use:   "ctl",
	Short: "Управление сессиями из другой программы (для дирижёра)",
}

var ctlListCmd = &cobra.Command{
	Use:   "list",
	Short: "Все сессии: номер, статус, стадия, recap",
	RunE: func(cmd *cobra.Command, args []string) error {
		snap, err := loadSnapshot()
		if err != nil {
			return err
		}
		if ctlJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(snap)
		}
		for _, s := range snap.Sessions {
			status := s.Status
			if s.Stage != "" {
				status += " · " + s.Stage
			}
			if s.IdleMinutes > 0 {
				status += fmt.Sprintf(" (%d мин)", s.IdleMinutes)
			}
			role := ""
			if s.Conductor {
				role = " [дирижёр]"
			} else if s.Assistant {
				role = " [помощник пользователя — не трогать]"
			} else if s.Group == session.GroupQA {
				role = " [QA]"
			} else if s.Group == session.GroupDev {
				role = " [разработка]"
			} else if s.Group != "" {
				role = " [" + s.Group + "]"
			}
			fmt.Printf("#%d %s%s — %s | %s | %s\n", s.Number, s.Title, role, status, s.Branch, s.Path)
			if s.Menu != nil {
				fmt.Printf("   меню (%d мин): %s → cs ctl answer %d <номер|recommended|текст>\n", s.MenuMinutes, s.Menu.Summary(), s.Number)
			}
			if s.Note != "" {
				fmt.Printf("   заметка: %s\n", s.Note)
			}
			if s.Recap != "" {
				fmt.Printf("   recap: %s\n", s.Recap)
			}
		}
		return nil
	},
}

var ctlReadCmd = &cobra.Command{
	Use:   "read <номер>",
	Short: "Последние сообщения сессии (твои и ответы агента)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		snap, err := loadSnapshot()
		if err != nil {
			return err
		}
		e, err := findSession(snap, args[0])
		if err != nil {
			return err
		}
		raw, err := capture(tmuxName(e), true)
		if err != nil {
			return err
		}
		if ctlLines < 1 {
			ctlLines = 1
		}
		if ctlTurns < 1 {
			ctlTurns = 1
		}
		for _, l := range session.ParseExchange(raw, ctlTurns, ctlLines) {
			fmt.Println(l.Text)
		}
		if menu, ok := session.ParseChoiceMenu(raw); ok {
			fmt.Printf("\n[открыто меню выбора] %s\nответить: cs ctl answer %d <номер|recommended|текст>\n", menu.Summary(), e.Number)
		}
		return nil
	},
}

var ctlSendCmd = &cobra.Command{
	Use:   "send <номер> <текст>",
	Short: "Отправить сообщение в сессию (свежий черновик пользователя не трогает, забытый — стирает)",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		snap, err := loadSnapshot()
		if err != nil {
			return err
		}
		e, err := findSession(snap, args[0])
		if err != nil {
			return err
		}
		if e.Assistant {
			return fmt.Errorf("#%d — помощник пользователя: в него пишет только пользователь", e.Number)
		}
		name := tmuxName(e)
		text := strings.TrimSpace(strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(strings.Join(args[1:], " ")))
		if text == "" {
			return fmt.Errorf("пустое сообщение")
		}
		if err := checkSendText(text); err != nil {
			return fmt.Errorf("#%d: %v", e.Number, err)
		}
		// A choice menu does not take messages: the text is the answer to it.
		if raw, err := capture(name, false); err == nil {
			if menu, ok := session.ParseChoiceMenu(raw); ok && !menu.PermissionLike() {
				if e.Conductor {
					return fmt.Errorf("#%d — дирижёр, в его меню выбирает только пользователь", e.Number)
				}
				fmt.Printf("в #%d открыто меню выбора — отвечаю на него (дальше лучше cs ctl answer)\n", e.Number)
				return answerMenu(e, name, menu, text)
			}
		}
		// Wait until the agent is up (after restart/wake it takes a while): input box
		// visible and the screen settled. Typing earlier loses the Enter.
		raw, err := waitInputReady(name, time.Duration(ctlWait)*time.Second)
		if err != nil {
			return fmt.Errorf("#%d %s: %v%s", e.Number, e.Title, err, dumpScreen(name, "no-input"))
		}
		if session.ShowsApprovalPrompt(raw) {
			return fmt.Errorf("#%d показывает запрос разрешения — текст мог бы его подтвердить, не отправляю. Разрешения подтверждает только пользователь", e.Number)
		}
		target := "=" + name + ":"
		if d := session.ExtractDraft(raw); d != "" {
			stale := ctlForce
			if !stale && e.HasDraft {
				age := time.Duration(e.DraftIdleSec)*time.Second + time.Since(snap.UpdatedAt)
				stale = age >= session.StaleDraftAfter
			}
			if !stale {
				return fmt.Errorf("в поле ввода #%d черновик пользователя («%s»), его трогали меньше %d минут назад — не отправляю, попробуй позже",
					e.Number, d, int(session.StaleDraftAfter.Minutes()))
			}
			if err := eraseInput(name, target, d); err != nil {
				return fmt.Errorf("#%d %s: %v", e.Number, e.Title, err)
			}
			session.SaveWipedDraft(e.Title, d)
			fmt.Printf("стёр забытый черновик в #%d («%s»), он сохранён в ~/.claude-squad/wiped-drafts.log\n", e.Number, d)
		}
		if err := exec.Command("tmux", "send-keys", "-t", target, "-l", text).Run(); err != nil {
			return err
		}
		// Make sure the text landed in the box before pressing Enter.
		// Claude Code can take a few seconds to show typed text (right after a start or
		// a trust question): wait up to 10 s, or the text lands later without its Enter.
		typed := false
		for k := 0; k < 50 && !typed; k++ {
			time.Sleep(200 * time.Millisecond)
			if raw, err := capture(name, false); err == nil && session.ExtractDraft(raw) != "" {
				typed = true
			}
		}
		if !typed {
			return fmt.Errorf("#%d %s: текст не появился в поле ввода — агент не принимает ввод, попробуй позже%s", e.Number, e.Title, dumpScreen(name, "not-typed"))
		}
		// Press Enter and check the box got emptied; one retry, then an honest error.
		sent := false
		for attempt := 0; attempt < 2 && !sent; attempt++ {
			if err := exec.Command("tmux", "send-keys", "-t", target, "Enter").Run(); err != nil {
				return err
			}
			for k := 0; k < 20; k++ {
				time.Sleep(200 * time.Millisecond)
				if raw, err := capture(name, false); err == nil && session.ExtractDraft(raw) == "" {
					sent = true
					break
				}
			}
		}
		if !sent {
			return fmt.Errorf("#%d %s: текст лежит в поле ввода, Enter не сработал — НЕ отправлено%s", e.Number, e.Title, dumpScreen(name, "enter-lost"))
		}
		fmt.Printf("отправлено в #%d %s\n", e.Number, e.Title)
		return nil
	},
}

// checkProgram: --program must be a plain program path with flags — no shell syntax
// (it runs in a shell inside tmux) and no flags that switch permission checks off.
func checkProgram(p string) error {
	if p == "" {
		return nil
	}
	if strings.ContainsAny(p, ";|&$`<>(){}\n\\\"'") {
		return fmt.Errorf("--program: только путь к программе и флаги, без символов оболочки")
	}
	f := strings.Fields(p)
	if base := strings.ToLower(filepath.Base(f[0])); !strings.HasPrefix(base, "claude") && !strings.HasPrefix(base, "codex") {
		return fmt.Errorf("--program: воркер запускается только как claude/codex (а не «%s»)", f[0])
	}
	low := strings.ToLower(p)
	for _, bad := range []string{"dangerously", "bypass", "skip-permissions", "--permission-mode", "acceptedits", "--allowedtools", "--allowed-tools",
		"--settings", "--mcp-config", "--add-dir", "--plugin-dir", "--setting-sources", "--strict-mcp-config", "--disallowedtools"} {
		if strings.Contains(low, bad) {
			return fmt.Errorf("--program: флаг «%s» отключает проверки разрешений — так воркеров не запускаю", bad)
		}
	}
	return nil
}

// blockedSlash are Claude Code commands that change permissions, settings or the
// account: the conductor must not run them in a worker.
var blockedSlash = []string{"/permissions", "/allowed-tools", "/config", "/login", "/logout", "/hooks", "/mcp", "/remote-control",
	"/rc", "/sandbox", "/model", "/privacy-settings", "/terminal-setup", "/install-github-app",
	"/plugin", "/plugins", "/add-dir", "/agents", "/output-style", "/statusline", "/vim", "/upgrade", "/bashes", "/ide"}

// checkSendText refuses messages Claude Code would not treat as a message: "!…" runs
// a shell command in the worker without any permission prompt, and some slash
// commands change permissions or settings.
func checkSendText(text string) error {
	text = strings.TrimLeft(text, " \t\r\n\u00a0\u200b\ufeff")
	if strings.ContainsAny(text, "\r\x1b") {
		return fmt.Errorf("в сообщении управляющие символы (\\r, ESC) — не отправляю")
	}
	if strings.HasPrefix(text, "!") {
		return fmt.Errorf("сообщение начинается с «!» — Claude Code выполнил бы его как команду оболочки без запроса разрешения; не отправляю")
	}
	if strings.HasPrefix(text, "/") {
		cmd := strings.ToLower(strings.Fields(text)[0])
		for _, b := range blockedSlash {
			if cmd == b {
				return fmt.Errorf("%s меняет разрешения или настройки — это делает только пользователь", cmd)
			}
		}
	}
	return nil
}

// eraseInput clears unsent text from the agent's input box (backspaces from the end).
func eraseInput(name, target, draft string) error {
	for attempt := 0; attempt < 2; attempt++ {
		n := len([]rune(draft)) + strings.Count(draft, "\n") + 2
		_ = exec.Command("tmux", "send-keys", "-t", target, "C-e").Run()
		if err := exec.Command("tmux", "send-keys", "-t", target, "-N", strconv.Itoa(n), "BSpace").Run(); err != nil {
			return err
		}
		time.Sleep(300 * time.Millisecond)
		raw, err := capture(name, false)
		if err != nil {
			return err
		}
		if draft = session.ExtractDraft(raw); draft == "" {
			return nil
		}
	}
	return fmt.Errorf("не получилось стереть текст в поле ввода («%s») — не отправляю", draft)
}

var ctlMarkCmd = &cobra.Command{
	Use:   "mark <номер> user|wait|clear [причина]",
	Short: "Чья очередь у сессии: user — ждёт пользователя, wait — ждёт внешнего (ревью, CI), clear — снова ждёт дирижёра",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		mark := args[1]
		if mark != "user" && mark != "wait" && mark != "clear" {
			return fmt.Errorf("второй аргумент: user, wait или clear")
		}
		snap, err := loadSnapshot()
		if err != nil {
			return err
		}
		e, err := findSession(snap, args[0])
		if err != nil {
			return err
		}
		if e.Conductor || e.Assistant {
			return fmt.Errorf("#%d — дирижёр/помощник, отметки только для воркеров", e.Number)
		}
		reason := strings.TrimSpace(strings.Join(args[2:], " "))
		if _, err := session.SubmitSessionRequest(session.SessionRequest{Title: e.Title, Mark: mark, MarkReason: reason}); err != nil {
			return err
		}
		fmt.Printf("запрос принят: #%d %s — %s %s (снимется, когда агент снова начнёт работать)\n", e.Number, e.Title, mark, reason)
		return nil
	},
}

var ctlMRCmd = &cobra.Command{
	Use:   "mr [<номер> [<ссылка|!номер> [open|merged|closed|ignore]]]",
	Short: "MR сессий: без аргументов — все; с номером — MR сессии; со ссылкой — записать MR и его состояние",
	Long: `MR сессий. Squad сам замечает MR на экране воркера (ссылки …/-/merge_requests/N и «MR !N»)
и раз в 30 минут кладёт в сводку дирижёру «MR-обход» — MR, за которыми ещё надо следить.

  cs ctl mr                       — все сессии с MR
  cs ctl mr 27                    — MR сессии #27
  cs ctl mr 27 <ссылка>           — добавить MR (открыт)
  cs ctl mr 27 <ссылка> merged    — влит; когда влиты все MR сессии, Squad отмечает её
                                    «ждёт тебя · MR влиты — можно закрыть сессию (D)»
  cs ctl mr 27 !21352 closed      — закрыт без влития
  cs ctl mr 27 !21063 ignore      — не MR этой сессии (чужой, только упомянут) — не следить`,
	Args: cobra.MaximumNArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		snap, err := loadSnapshot()
		if err != nil {
			return err
		}
		show := func(e session.StatusEntry) {
			if len(e.MRs) == 0 {
				return
			}
			fmt.Printf("#%d %s\n", e.Number, e.Title)
			for _, r := range e.MRs {
				st := r.State
				if st == "" {
					st = "открыт"
				}
				fmt.Printf("  %s — %s\n", r.Ref, st)
			}
		}
		if len(args) == 0 {
			for _, e := range snap.Sessions {
				show(e)
			}
			return nil
		}
		e, err := findSession(snap, args[0])
		if err != nil {
			return err
		}
		if len(args) == 1 {
			if len(e.MRs) == 0 {
				fmt.Printf("#%d: MR не замечено\n", e.Number)
			}
			show(*e)
			return nil
		}
		if e.Conductor || e.Assistant {
			return fmt.Errorf("#%d — дирижёр/помощник, MR только у воркеров", e.Number)
		}
		ref := strings.TrimSpace(args[1])
		if !strings.Contains(ref, "/merge_requests/") && !regexp.MustCompile(`^!\d+$`).MatchString(ref) {
			return fmt.Errorf("MR — ссылка …/-/merge_requests/N или !N")
		}
		state := "open"
		if len(args) == 3 {
			state = args[2]
		}
		switch state {
		case "open", "merged", "closed", "ignore":
		default:
			return fmt.Errorf("состояние: open, merged, closed или ignore")
		}
		if _, err := session.SubmitSessionRequest(session.SessionRequest{Title: e.Title, MR: ref, MRState: state}); err != nil {
			return err
		}
		fmt.Printf("запрос принят: #%d %s — %s %s\n", e.Number, e.Title, ref, state)
		return nil
	},
}

var ctlGroupCmd = &cobra.Command{
	Use:   "group <номер> qa|dev|auto|<своя группа>",
	Short: "Группа сессии: qa — QA-проверка, dev — разработка, своё название, auto — угадывать по названию",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		g, ok := session.NormGroup(strings.Join(args[1:], " "))
		if !ok {
			return fmt.Errorf("группа: qa, dev, auto или своё название до 32 символов")
		}
		snap, err := loadSnapshot()
		if err != nil {
			return err
		}
		e, err := findSession(snap, args[0])
		if err != nil {
			return err
		}
		start := time.Now()
		if _, err := session.SubmitSessionRequest(session.SessionRequest{Title: e.Title, Group: g, SetGroup: true}); err != nil {
			return err
		}
		return waitRequest(e.Title, start, 15*time.Second, fmt.Sprintf("запрос принят: #%d %s", e.Number, e.Title), "выполнится, когда Squad освободится")
	},
}

var ctlNewCmd = &cobra.Command{
	Use:   "new",
	Short: "Открыть сессию в существующей папке (Squad создаст её за пару секунд)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if ctlNewDir == "" || ctlNewTitle == "" {
			return fmt.Errorf("нужны --dir и --title")
		}
		if err := checkProgram(ctlNewProg); err != nil {
			return err
		}
		if ctlNewCond || ctlNewAsst {
			if snap, err := session.ReadStatusSnapshot(); err == nil {
				for _, x := range snap.Sessions {
					if (ctlNewCond && x.Conductor) || (ctlNewAsst && x.Assistant) {
						return fmt.Errorf("дирижёр/помощник уже есть (#%d) — второй не открываю", x.Number)
					}
				}
			}
		}
		dir, err := filepath.Abs(ctlNewDir)
		if err != nil {
			return err
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return fmt.Errorf("папки %s нет — сначала создай площадку/worktree", dir)
		}
		if ctlNewPrompt != "" {
			if err := checkSendText(strings.TrimSpace(ctlNewPrompt)); err != nil {
				return fmt.Errorf("--prompt: %v", err)
			}
		}
		if snap, err := session.ReadStatusSnapshot(); err == nil {
			for _, s := range snap.Sessions {
				if s.Title == ctlNewTitle {
					return fmt.Errorf("сессия «%s» уже есть (#%d)", s.Title, s.Number)
				}
			}
		}
		start := time.Now()
		_, err = session.SubmitSessionRequest(session.SessionRequest{
			Title: ctlNewTitle, Dir: dir, Branch: ctlNewBranch, Prompt: ctlNewPrompt,
			Color: ctlNewColor, Note: ctlNewNote, Program: ctlNewProg, Conductor: ctlNewCond, Assistant: ctlNewAsst, Group: ctlNewGroup,
		})
		if err != nil {
			return err
		}
		return waitRequest(ctlNewTitle, start, 30*time.Second,
			fmt.Sprintf("запрос принят: «%s» в %s", ctlNewTitle, dir),
			"запрос ждёт в очереди (до суток) и выполнится сам, когда Squad освободится — проверь cs ctl list")
	},
}

var ctlKillCmd = &cobra.Command{
	Use:   "kill <номер>",
	Short: "Закрыть сессию (папка и ветка остаются)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		snap, err := loadSnapshot()
		if err != nil {
			return err
		}
		e, err := findSession(snap, args[0])
		if err != nil {
			return err
		}
		if e.Conductor || e.Assistant {
			return fmt.Errorf("#%d закрывает только пользователь", e.Number)
		}
		start := time.Now()
		if _, err := session.SubmitSessionRequest(session.SessionRequest{Title: e.Title, Kill: true}); err != nil {
			return err
		}
		return waitRequest(e.Title, start, 30*time.Second, fmt.Sprintf("запрос принят: #%d %s будет закрыта", e.Number, e.Title),
			"запрос ждёт в очереди и выполнится сам, когда Squad освободится — проверь cs ctl list")
	},
}

// waitRequest waits for Squad's result of a new/kill request: the real outcome
// ("создана #N", "уже есть", an error) instead of a blind «принят».
func waitRequest(title string, start time.Time, wait time.Duration, accepted, later string) error {
	for time.Since(start) < wait {
		time.Sleep(time.Second)
		if res, ok := session.ReadCtlResult(title, start); ok {
			if strings.HasPrefix(res, "ошибка") {
				return fmt.Errorf("«%s»: %s", title, res)
			}
			fmt.Printf("«%s»: %s\n", title, res)
			return nil
		}
	}
	why := ""
	if snap, err := session.ReadStatusSnapshot(); err == nil && snap.UIWaiting != "" {
		why = " (сейчас: " + snap.UIWaiting + ")"
	}
	fmt.Printf("%s — Squad пока не выполнил%s; %s\n", accepted, why, later)
	return nil
}

func ctlActionCmd(use, short string, build func(title string) session.SessionRequest) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <номер>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			snap, err := loadSnapshot()
			if err != nil {
				return err
			}
			e, err := findSession(snap, args[0])
			if err != nil {
				return err
			}
			start := time.Now()
			if _, err := session.SubmitSessionRequest(build(e.Title)); err != nil {
				return err
			}
			// Wait for Squad to act. It handles wake/restart even while a window is
			// open in it; only the session the user is inside waits for them to leave.
			for time.Since(start) < 90*time.Second {
				time.Sleep(time.Second)
				if res, ok := session.ReadCtlResult(e.Title, start); ok {
					if strings.HasPrefix(res, "ошибка") {
						return fmt.Errorf("#%d %s: %s", e.Number, e.Title, res)
					}
					fmt.Printf("#%d %s: %s\n", e.Number, e.Title, res)
					return nil
				}
			}
			why := "Squad не ответил"
			if snap2, err := session.ReadStatusSnapshot(); err == nil && snap2.UIWaiting != "" {
				why = snap2.UIWaiting
			}
			return fmt.Errorf("#%d %s: НЕ выполнено за 90 с (%s). Запрос ждёт в очереди до 10 минут, потом отменится — проверь позже через cs ctl list", e.Number, e.Title, why)
		},
	}
}

var ctlWakeCmd = ctlActionCmd("wake", "Разбудить спящую/остановленную сессию (тот же разговор)",
	func(title string) session.SessionRequest { return session.SessionRequest{Title: title, Wake: true} })

var ctlRestartCmd = ctlActionCmd("restart", "Перезапустить агента с тем же разговором (переподключить MCP)",
	func(title string) session.SessionRequest { return session.SessionRequest{Title: title, Restart: true} })

// terminalApp maps TERM_PROGRAM to the macOS app name.
func terminalApp(termProgram string) string {
	switch termProgram {
	case "Apple_Terminal", "":
		return "Terminal"
	case "iTerm.app":
		return "iTerm"
	case "vscode":
		return "Visual Studio Code"
	case "WarpTerminal":
		return "Warp"
	case "ghostty":
		return "Ghostty"
	default:
		return termProgram
	}
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

var ctlFocusCmd = &cobra.Command{
	Use:   "focus <номер|название>",
	Short: "Открыть сессию в Squad на весь экран (клик по уведомлению); запустит Squad, если он закрыт",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		snap, _ := session.ReadStatusSnapshot()
		running := snap != nil && time.Since(snap.UpdatedAt) < time.Minute
		title := strings.TrimPrefix(args[0], "#")
		if snap != nil {
			if e, err := findSession(snap, args[0]); err == nil {
				title = e.Title
			} else if _, numErr := strconv.Atoi(title); numErr == nil {
				return err
			}
		}
		if _, err := session.SubmitSessionRequest(session.SessionRequest{Title: title, Focus: true, At: time.Now()}); err != nil {
			return err
		}
		if running {
			app := terminalApp(snap.Terminal)
			_ = exec.Command("/usr/bin/open", "-a", app).Run()
			if snap.UIWaiting != "" {
				fmt.Printf("Squad занят (%s) — «%s» откроется, когда выйдешь\n", snap.UIWaiting, title)
			}
			return nil
		}
		// Squad is not running: start it in a new terminal window; it picks up the request.
		// Through the sq wrapper when it is there: without it Squad would get no update
		// signal and no remote actions.
		start := "sq"
		home, _ := os.UserHomeDir()
		_, sqErr := os.Stat(filepath.Join(home, "bin", "sq"))
		if snap != nil && snap.Dir != "" {
			if sqErr == nil {
				start = "SQ_DIR=" + shellQuote(snap.Dir) + " " + shellQuote(filepath.Join(home, "bin", "sq"))
			} else {
				self, _ := os.Executable()
				if self == "" {
					self = "cs"
				}
				start = "cd " + shellQuote(snap.Dir) + " && " + shellQuote(self)
			}
		}
		app := "Terminal"
		if snap != nil {
			app = terminalApp(snap.Terminal)
		}
		script := fmt.Sprintf(`tell application "Terminal" to do script %q`, start) + "\n" + `tell application "Terminal" to activate`
		if app == "iTerm" {
			script = fmt.Sprintf(`tell application "iTerm" to create window with default profile command %q`, "/bin/zsh -lic "+shellQuote(start))
		}
		return exec.Command("/usr/bin/osascript", "-e", script).Run()
	},
}

// dumpScreen saves what the session shows (for a bug report) and returns a
// hint with the file path, or "".
func dumpScreen(name, why string) string {
	raw, err := capture(name, false)
	if err != nil {
		return ""
	}
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".claude-squad", "ctl-debug")
	if os.MkdirAll(dir, 0700) != nil {
		return ""
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%s-%s.txt", time.Now().Format("0102-150405"), why, name))
	if os.WriteFile(path, []byte(raw), 0600) != nil {
		return ""
	}
	return " (экран сохранён: " + path + ")"
}

// waitInputReady waits for the agent's input box and a settled screen.
func waitInputReady(name string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	prev := ""
	stable := 0
	for {
		raw, err := capture(name, false)
		if err != nil {
			return "", err
		}
		if session.InputBoxVisible(raw) || session.ShowsApprovalPrompt(raw) {
			if raw == prev {
				stable++
			} else {
				stable = 0
			}
			// Settled for ~1 s, or it is working (spinner keeps changing the screen)
			// for a while: Claude Code accepts a message while it works.
			if stable >= 2 || (time.Until(deadline) < timeout-8*time.Second && session.InputBoxVisible(raw)) {
				return raw, nil
			}
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("агент так и не показал поле ввода за %s — НЕ отправлено", timeout)
		}
		prev = raw
		time.Sleep(500 * time.Millisecond)
	}
}

func init() {
	ctlSendCmd.Flags().IntVar(&ctlWait, "wait", 120, "сколько секунд ждать, пока агент будет готов")
	ctlListCmd.Flags().BoolVar(&ctlJSON, "json", false, "вывод в JSON")
	ctlReadCmd.Flags().IntVar(&ctlTurns, "turns", 2, "сколько последних сообщений пользователя показать")
	ctlReadCmd.Flags().IntVar(&ctlLines, "lines", 80, "максимум строк")
	ctlSendCmd.Flags().BoolVar(&ctlForce, "force", false, "стереть черновик в поле и отправить, даже если его только что трогали")
	f := ctlNewCmd.Flags()
	f.StringVar(&ctlNewDir, "dir", "", "папка (worktree/площадка), где работать")
	f.StringVar(&ctlNewTitle, "title", "", "название сессии (до 32 символов)")
	f.StringVar(&ctlNewBranch, "branch", "", "подпись ветки в списке")
	f.StringVar(&ctlNewPrompt, "prompt", "", "первое сообщение агенту, когда он будет готов")
	f.StringVar(&ctlNewColor, "color", "", "red|orange|yellow|green|cyan|blue|purple|pink")
	f.StringVar(&ctlNewNote, "note", "", "описание сессии")
	f.StringVar(&ctlNewGroup, "group", "", "группа: qa (QA-проверка) или dev (разработка); без неё — угадывается по названию")
	f.StringVar(&ctlNewProg, "program", "", "программа вместо claude по умолчанию")
	f.BoolVar(&ctlNewCond, "conductor", false, "это дирижёр")
	f.BoolVar(&ctlNewAsst, "assistant", false, "это помощник пользователя (дирижёр в него не пишет)")
	for _, c := range []*cobra.Command{ctlListCmd, ctlReadCmd, ctlSendCmd, ctlNewCmd, ctlKillCmd, ctlWakeCmd, ctlRestartCmd, ctlFocusCmd, ctlMarkCmd, ctlMRCmd, ctlGroupCmd} {
		c.SilenceUsage = true
		c.SilenceErrors = true
	}
	ctlCmd.AddCommand(ctlListCmd, ctlReadCmd, ctlSendCmd, ctlNewCmd, ctlKillCmd, ctlWakeCmd, ctlRestartCmd, ctlFocusCmd, ctlMarkCmd, ctlMRCmd, ctlGroupCmd)
	rootCmd.AddCommand(ctlCmd)
}

var ctlCapacityCmd = &cobra.Command{
	Use:   "capacity",
	Short: "Есть ли ресурс взять ещё задачу: свободные места и лимиты (без токенов)",
	Long: `Есть ли ресурс взять ещё задачу. Место занимает сессия-воркер, которая работает или
только что закончила шаг (простаивает меньше 30 минут): в работе, в ожидании, ждёт дирижёра,
ждёт тебя, меню. Простаивающая дольше 30 минут, спящая и на паузе места не занимают.
Дирижёр и помощник мест не занимают. Мест — "max_active_sessions" в конфиге (по умолчанию 6).
Это подсказка: cs ctl new сверх лимита не запрещён.
Ещё проверяется место на диске: меньше 20 ГБ — не больше одного нового клона окружения,
меньше 10 ГБ — «ресурса нет», ничего нового не запускать, уборку решает пользователь.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		snap, err := loadSnapshot()
		if err != nil {
			return err
		}
		c := session.ComputeCapacity(snap, config.LoadConfig().MaxActive())
		if c.OK {
			fmt.Printf("есть ресурс: %s\n", c.Reason)
		} else {
			fmt.Printf("ресурса нет: %s\n", c.Reason)
		}
		if len(c.BusyList) > 0 {
			fmt.Printf("места заняты (%d из %d):\n", c.Busy, c.Max)
			for _, b := range c.BusyList {
				fmt.Println("  " + b)
			}
		}
		if len(c.IdleList) > 0 {
			fmt.Println("простаивают дольше 30 мин (мест не занимают):")
			for _, b := range c.IdleList {
				fmt.Println("  " + b)
			}
		}
		if c.DiskFreeGB >= 0 {
			fmt.Printf("диск: свободно %.0f ГБ\n", c.DiskFreeGB)
		}
		if snap.Usage5hPct == 0 && snap.UsageWeekPct == 0 {
			fmt.Println("(лимиты не откалиброваны — cs usage --calibrate 5h=NN --calibrate week=NN)")
		}
		return nil
	},
}

func init() {
	ctlCapacityCmd.SilenceUsage = true
	ctlCmd.AddCommand(ctlCapacityCmd)
}
