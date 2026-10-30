package session

import (
	"claude-squad/session/tmux"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ansiRe matches terminal escape sequences (colors, cursor moves, OSC titles).
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[()][A-Za-z0-9]|\x1b[=>]`)

// StripANSI removes terminal escape sequences from s.
func StripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

// Strings Claude Code shows while it waits for the user to approve something.
var needsInputMarkers = []string{
	"No, and tell Claude what to do differently",
	"Do you want to proceed?",
	"Do you want to make this edit",
	"Do you want to create",
}

var spaceRe = regexp.MustCompile(`\s+`)

// extractRecap returns the text of the last "recap:" block Claude Code printed
// on screen, joined into one line, or "" if there is none.
func extractRecap(plain string) string {
	idx := strings.LastIndex(plain, "recap:")
	if idx < 0 {
		return ""
	}
	var parts []string
	for n, line := range strings.Split(plain[idx+len("recap:"):], "\n") {
		t := strings.TrimSpace(line)
		if n > 0 && (t == "" || strings.HasPrefix(t, "❯") || strings.HasPrefix(t, "─") ||
			strings.HasPrefix(t, ">") || strings.HasPrefix(t, "✔") || strings.HasPrefix(t, "●")) {
			break
		}
		parts = append(parts, t)
	}
	recap := strings.Join(parts, " ")
	recap = strings.ReplaceAll(recap, "(disable recaps in /config)", "")
	recap = spaceRe.ReplaceAllString(recap, " ")
	return strings.TrimSpace(recap)
}

// tailLines returns up to n last non-empty lines of plain, skipping Claude Code's
// input box and footer so the overview shows the conversation itself.
func tailLines(plain string, n int) []string {
	lines := strings.Split(strings.TrimRight(plain, "\n"), "\n")
	// Cut the input box: everything from the last prompt border line down.
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-12; i-- {
		t := strings.TrimSpace(lines[i])
		if strings.HasPrefix(t, "❯") && !menuOptionRe.MatchString(strings.TrimSpace(strings.TrimPrefix(t, "❯"))+" ") {
			cut := i
			if cut > 0 && strings.HasPrefix(strings.TrimSpace(lines[cut-1]), "─") {
				cut--
			}
			lines = lines[:cut]
			break
		}
	}
	var out []string
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		t := strings.TrimRight(lines[i], " ")
		if strings.TrimSpace(t) == "" || strings.HasPrefix(strings.TrimSpace(t), "─") {
			continue
		}
		out = append(out, t)
	}
	for l, r := 0, len(out)-1; l < r; l, r = l+1, r-1 {
		out[l], out[r] = out[r], out[l]
	}
	return out
}

// dimMarkers are SGR sequences Claude Code uses for its grey "ghost" suggestion text.
var dimMarkers = []string{"\x1b[2m", "\x1b[2;", ";2m", "\x1b[90m", "38;5;8m", "38;5;24", "38;5;59m", "38;5;102m"}

var trueColorRe = regexp.MustCompile(`38;2;(\d+);(\d+);(\d+)`)

// hasGreyTrueColor reports whether s uses a mid-grey 24-bit foreground color.
func hasGreyTrueColor(s string) bool {
	for _, m := range trueColorRe.FindAllStringSubmatch(s, -1) {
		r, _ := strconv.Atoi(m[1])
		g, _ := strconv.Atoi(m[2])
		b, _ := strconv.Atoi(m[3])
		if r >= 80 && r <= 180 && abs(r-g) < 20 && abs(g-b) < 20 {
			return true
		}
	}
	return false
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// findInputLine locates Claude Code's input line ("❯ ...") near the bottom of the
// screen and returns its index in lines, or -1.
var menuOptionRe = regexp.MustCompile(`^\s*\d+\.\s`)

func findInputLine(lines []string) int {
	for n := len(lines) - 1; n >= 0 && n >= len(lines)-16; n-- {
		idx := strings.Index(lines[n], "❯")
		if idx < 0 {
			// Older Claude Code draws the input as "│ > text │" inside a box.
			plain := StripANSI(lines[n])
			if t := strings.TrimLeft(plain, " "); strings.HasPrefix(t, "│ > ") || t == "│ >" || strings.HasPrefix(t, "│ >\u00a0") {
				return n
			}
			continue
		}
		if strings.TrimSpace(StripANSI(lines[n][:idx])) != "" {
			continue
		}
		// "❯ 1. Yes" is the cursor of a choice menu (permission prompt), not the input box.
		after := strings.ReplaceAll(StripANSI(lines[n][idx+len("❯"):]), "\u00a0", " ")
		if menuOptionRe.MatchString(after) {
			return -1
		}
		return n
	}
	return -1
}

var sgrRe = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// textStyleAt returns whether the first visible character after position from in
// line is drawn dim or in grey. tmux capture-pane -e only emits a code when the
// style changes, so a dim "❯" carries its dimness on to the text after it: the
// effective style has to be computed from all codes before that character.
func textStyleAt(line string, from int) (dim, grey bool) {
	dim, grey, _, _ = textStyleFull(line, from)
	return dim, grey
}

// textStyleFull is textStyleAt that also tells whether that character is drawn
// in reverse video (the cursor) and where it ends.
func textStyleFull(line string, from int) (dim, grey, inverse bool, next int) {
	// Find the first visible (non-space) character after from.
	start := -1
	for i := from; i < len(line); {
		if loc := ansiRe.FindStringIndex(line[i:]); loc != nil && loc[0] == 0 {
			i += loc[1]
			continue
		}
		r := []rune(line[i:])[0]
		if r != ' ' && r != '\u00a0' {
			start = i
			break
		}
		i += len(string(r))
	}
	if start < 0 {
		return false, false, false, -1
	}
	fgGrey := false
	for _, m := range sgrRe.FindAllStringSubmatchIndex(line[:start], -1) {
		params := strings.Split(line[m[2]:m[3]], ";")
		for k := 0; k < len(params); k++ {
			switch p := params[k]; p {
			case "", "0":
				dim, fgGrey, inverse = false, false, false
			case "7":
				inverse = true
			case "27":
				inverse = false
			case "2":
				dim = true
			case "22":
				dim = false
			case "90", "37":
				fgGrey = p == "90"
			case "39":
				fgGrey = false
			case "38":
				if k+2 < len(params) && params[k+1] == "5" {
					n, _ := strconv.Atoi(params[k+2])
					fgGrey = n == 8 || (n >= 240 && n <= 250) || n == 59 || n == 102
					k += 2
				} else if k+4 < len(params) && params[k+1] == "2" {
					fgGrey = hasGreyTrueColor("38;2;" + params[k+2] + ";" + params[k+3] + ";" + params[k+4])
					k += 4
				}
			default:
				if n, err := strconv.Atoi(p); err == nil && n >= 30 && n <= 97 && (n <= 37 || n >= 90) {
					fgGrey = n == 90
				}
			}
		}
	}
	return dim, fgGrey, inverse, start + len(string([]rune(line[start:])[0]))
}

// isGhostText reports whether the text after the prompt sign is Claude Code's
// greyed-out suggestion rather than text the user typed.
func isGhostText(line string, after int) bool {
	dim, grey, inverse, next := textStyleFull(line, after)
	if inverse && !dim && !grey && next > 0 {
		// The cursor sits on the first letter of the greyed-out suggestion
		// ("❯ [T]ry \"fix lint\""): judge by the letters after it.
		if d2, g2, _, n2 := textStyleFull(line, next); n2 > 0 {
			return d2 || g2
		}
	}
	return dim || grey
}

// promptEnd is the byte offset right after the input prompt sign in line.
func promptEnd(line string) int {
	if i := strings.Index(line, "❯"); i >= 0 {
		return i + len("❯")
	}
	if i := strings.Index(line, "│ >"); i >= 0 {
		return i + len("│ >")
	}
	return 0
}

// extractDraft returns text the user already typed into Claude Code's input box
// but did not send (multi-line drafts included), or "".
func extractDraft(raw string) string {
	lines := strings.Split(strings.TrimRight(raw, "\n"), "\n")
	n := findInputLine(lines)
	if n < 0 {
		return ""
	}
	after := promptEnd(lines[n])
	rest := lines[n][after:]
	first := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(strings.ReplaceAll(StripANSI(rest), "\u00a0", " ")), "│"))
	if first == "" || isGhostText(lines[n], after) {
		return ""
	}
	parts := []string{first}
	for _, l := range lines[n+1:] {
		t := strings.TrimSpace(StripANSI(l))
		if t == "" || strings.HasPrefix(t, "─") || strings.HasPrefix(t, "╰") || strings.HasPrefix(t, "╭") {
			break
		}
		if t = strings.TrimSpace(strings.Trim(t, "│")); t == "" {
			break
		}
		parts = append(parts, t)
	}
	return strings.Join(parts, "\n")
}

// extractSuggestion returns the prompt suggestion Claude Code shows greyed out in
// its empty input box ("❯ fix MR, просто отправь ответ"), or "" if there is none.
func extractSuggestion(raw string) string {
	lines := strings.Split(strings.TrimRight(raw, "\n"), "\n")
	n := findInputLine(lines)
	if n < 0 {
		return ""
	}
	after := promptEnd(lines[n])
	rest := lines[n][after:]
	if text := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(strings.ReplaceAll(StripANSI(rest), "\u00a0", " ")), "│")); text != "" && isGhostText(lines[n], after) {
		return text
	}
	return "" // empty box, or text the user typed (a draft), not a suggestion
}

// ApplyPaneContent updates the activity info derived from the session's screen.
// It must be called from the UI goroutine.
func (i *Instance) ApplyPaneContent(content string, hasPrompt bool) (recapChanged bool) {
	return i.applyPane(content, hasPrompt, false)
}

// ApplyPaneUpdate is ApplyPaneContent for a monitor tick; updated tells whether
// the screen changed since the previous tick (the agent is working).
func (i *Instance) ApplyPaneUpdate(content string, hasPrompt, updated bool) (recapChanged bool) {
	return i.applyPane(content, hasPrompt, updated)
}

// networkMarkers are errors shown when the agent or its tools cannot reach the network.
var networkMarkers = []string{
	"no such host", "could not resolve host", "connection closed", "connection refused",
	"econnrefused", "enotfound", "etimedout", "network is unreachable",
	"unable to connect to api", "api error: connection", "connection error",
	"включите vpn", "включить vpn", "нет сети", "не открывается (no such host",
}

func detectNetworkIssue(plain string) bool {
	lines := strings.Split(strings.TrimRight(plain, "\n"), "\n")
	if len(lines) > 18 {
		lines = lines[len(lines)-18:]
	}
	text := strings.ToLower(strings.Join(lines, "\n"))
	for _, m := range networkMarkers {
		if strings.Contains(text, m) {
			return true
		}
	}
	return false
}

// spinnerGlyphs start Claude Code's "working" line (✻ Pondering… (12s · esc to interrupt)).
const spinnerGlyphs = "✻✶✳✢✽·*+"

var digitsRe = regexp.MustCompile(`[0-9]+`)

// progressSignature summarizes what the current turn shows, ignoring spinners and
// timers: when it stays the same for minutes while the agent "works", it is stuck.
func progressSignature(turn string) string {
	var b strings.Builder
	for _, l := range strings.Split(turn, "\n") {
		t := strings.TrimSpace(l)
		if t == "" || strings.Contains(strings.ToLower(t), "esc to interrupt") {
			continue
		}
		if r, _ := utf8.DecodeRuneInString(t); strings.ContainsRune(spinnerGlyphs, r) && strings.Contains(t, "…") {
			continue
		}
		b.WriteString(digitsRe.ReplaceAllString(t, "#"))
		b.WriteByte('\n')
	}
	return b.String()
}

// classifyNetError tells whether the current turn shows a network error, and
// whether it is an MCP server that lost its connection (fixed by restarting the agent).
func classifyNetError(turn string) (network, mcp bool) {
	lines := strings.Split(strings.TrimRight(turn, "\n"), "\n")
	if len(lines) > 25 {
		lines = lines[len(lines)-25:]
	}
	for _, l := range lines {
		low := strings.ToLower(l)
		hit := false
		for _, m := range networkMarkers {
			if strings.Contains(low, m) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		network = true
		if strings.Contains(low, "mcp") || strings.Contains(low, "connection closed") {
			mcp = true
		}
	}
	return network, mcp
}

// lastToolIsMCP reports whether the latest tool call in the turn is an MCP tool
// ("⏺ gitlab - get_merge_request (MCP)(...)").
func lastToolIsMCP(turn string) bool {
	lines := strings.Split(turn, "\n")
	for k := len(lines) - 1; k >= 0; k-- {
		t := strings.TrimSpace(lines[k])
		if strings.HasPrefix(t, "⏺") || strings.HasPrefix(t, "●") {
			if strings.Contains(t, "(MCP)") {
				return true
			}
			if strings.Contains(t, "(") { // another tool call
				return false
			}
		}
	}
	return false
}

var (
	ciWaitRe   = regexp.MustCompile(`(?i)\b(gh run watch|gh pr checks|glab ci|pipeline|ci status|ci view)\b`)
	sleepRe    = regexp.MustCompile(`(?i)(^|[\s;&(])sleep\s+\d|\bwait\b|\btimeout\s+\d`)
	pollRe     = regexp.MustCompile(`(?i)\b(until|while)\b.*\bdo\b|\bwatch\s`)
	bgFooterRe = regexp.MustCompile(`(?i)\b\d+\s+(background (tasks?|shells?)|shells?)\b|\bbackground tasks?\b`)
)

// waitReason tells what the agent itself is waiting for (a timer, CI, a
// background task), as opposed to waiting for the user. Empty when nothing.
func waitReason(turn, plain string) string {
	lines := strings.Split(turn, "\n")
	for k := len(lines) - 1; k >= 0; k-- {
		t := strings.TrimSpace(lines[k])
		if !strings.HasPrefix(t, "⏺") && !strings.HasPrefix(t, "●") {
			continue
		}
		if !strings.Contains(t, "(") {
			continue // agent text, not a tool call
		}
		switch {
		case strings.Contains(t, "Monitor(") || strings.Contains(t, "BashOutput("):
			return "фоновая задача"
		case !strings.Contains(t, "Bash("):
			return ""
		case ciWaitRe.MatchString(t):
			return "пайплайн"
		case pollRe.MatchString(t):
			return "опрос"
		case sleepRe.MatchString(t):
			return "таймер"
		}
		return ""
	}
	// Idle, but with background shells still running (Claude Code's footer).
	tail := strings.Split(strings.TrimRight(plain, "\n"), "\n")
	if len(tail) > 4 {
		tail = tail[len(tail)-4:]
	}
	if bgFooterRe.MatchString(strings.Join(tail, "\n")) {
		return "фоновая задача"
	}
	return ""
}

// WaitsOnNetwork reports whether the current step is an MCP call or shows a
// network error — the kind of step a VPN drop leaves hanging.
func (i *Instance) WaitsOnNetwork() bool { return i.turnMCPCall || i.turnNetIssue }

// TurnSignature is progressSignature of the screen at the last monitor tick.
func (i *Instance) TurnSignature() string { return i.turnSig }

// TurnNetworkError reports a network error in the current turn and whether an MCP
// server lost its connection.
func (i *Instance) TurnNetworkError() (network, mcp bool) { return i.turnNetIssue, i.turnMCPIssue }

// Interrupt presses Esc in the session (stops the step the agent is stuck on).
func (i *Instance) Interrupt() error {
	if !i.started || i.tmuxSession == nil {
		return fmt.Errorf("сессия не запущена")
	}
	return i.tmuxSession.SendKeys("\x1b")
}

// currentTurn returns the part of the screen after the user's last message
// (excluding the input box), i.e. what belongs to the task in progress.
func currentTurn(plain string) string {
	lines := strings.Split(strings.TrimRight(strings.ReplaceAll(plain, "\u00a0", " "), "\n"), "\n")
	end := len(lines)
	if n := findInputLine(lines); n >= 0 {
		end = n
	}
	for i := end - 1; i >= 0; i-- {
		if isUserPromptLine(strings.TrimSpace(lines[i])) {
			return strings.Join(lines[i:], "\n")
		}
	}
	return plain
}

var interruptedMarkers = []string{"What should Claude do instead?", "Interrupted by user"}

func (i *Instance) applyPane(content string, hasPrompt, updated bool) (recapChanged bool) {
	plain := StripANSI(content)
	if i.NoteMRs(plain) {
		recapChanged = true // saved with the recap
	}
	turn := currentTurn(plain)
	bottom := lastLines(plain, 30)
	menu, isMenu := ParseChoiceMenu(content)
	permLike := isMenu && menu.PermissionLike()
	needs := hasPrompt || permLike
	if !isMenu {
		for _, m := range needsInputMarkers {
			if strings.Contains(bottom, m) {
				needs = true
				break
			}
		}
	}
	if needs && !i.NeedsInput {
		i.NeedsSince = time.Now()
	}
	i.NeedsInput = needs
	i.PermissionCmd = ""
	if permLike {
		i.PermissionCmd = redactSecrets("диалог: " + menu.Question)
	} else if needs {
		i.PermissionCmd = redactSecrets(PermissionRequest(content))
	}
	// Only a recap written after the user's last message describes the current task.
	if r := extractRecap(turn); r != "" {
		if r != i.Recap {
			i.Recap = r
			recapChanged = true
		}
		i.RecapStale = false
	} else if updated || extractRecap(plain) != "" {
		// The agent is working on something new (or the only recap on screen is from
		// before the last message): the old stage no longer describes the task.
		i.RecapStale = true
	}
	i.Interrupted = false
	tail := plain
	if lines := strings.Split(plain, "\n"); len(lines) > 12 {
		tail = strings.Join(lines[len(lines)-12:], "\n")
	}
	for _, m := range interruptedMarkers {
		if strings.Contains(tail, m) {
			i.Interrupted = true
		}
	}
	i.NetworkIssue = detectNetworkIssue(plain)
	i.turnSig = progressSignature(turn)
	i.turnNetIssue, i.turnMCPIssue = classifyNetError(turn)
	i.turnMCPCall = lastToolIsMCP(turn)
	i.WaitReason = waitReason(turn, plain)
	i.LimitHit, i.LimitReset = detectLimit(turn, time.Now())
	i.TailLines = tailLines(plain, 40)
	i.Suggestion = extractSuggestion(content)
	if d := extractDraft(content); d != i.Draft {
		i.Draft, i.DraftSince = d, time.Now()
	}
	i.inputVisible = findInputLine(strings.Split(strings.TrimRight(plain, "\n"), "\n")) >= 0
	i.CallsUser = ""
	if i.Conductor || i.Assistant {
		i.CallsUser = callsUser(turn)
	}
	i.TrustPrompt = !needs && !isMenu && isTrustDialog(lastLines(plain, 25))
	if isMenu && !needs && !i.TrustPrompt {
		// A different question (the next tab of the same AskUserQuestion) starts its
		// own wait: its age and escalation must not carry over from the previous one.
		if i.Menu == nil || MenuKey(*i.Menu) != MenuKey(menu) {
			i.MenuSince, i.MenuEscalated = time.Now(), false
		}
		i.Menu = &menu
	} else {
		i.Menu, i.MenuEscalated = nil, false
	}
	return recapChanged
}

// StatusKind identifies the session state for coloring.
type StatusKind int

const (
	StatusWorking StatusKind = iota
	StatusWaiting
	StatusNeedsInput
	StatusPaused
	StatusStarting
	// StatusOnHold: the agent waits for something by itself (CI, timer, background task).
	StatusOnHold
	// StatusConductor: a worker finished a step and waits for the conductor, not the user.
	StatusConductor
	// StatusStandby: the conductor is idle and has nothing for the user.
	StatusStandby
)

// StatusLabel returns a short human label of what the agent is doing right now.
func (i *Instance) StatusLabel() (string, StatusKind) {
	switch {
	case i.Status == Paused && i.Hibernated:
		return "спит", StatusPaused
	case i.Status == Paused:
		return "на паузе", StatusPaused
	case i.Status == Loading:
		return "запускается", StatusStarting
	case i.Status == Running && i.WaitReason != "":
		return "в ожидании · " + i.WaitReason, StatusOnHold
	case i.Status == Running:
		return "в работе", StatusWorking
	case i.NeedsInput && i.PermissionCmd != "" && i.PermissionCmd != "?":
		return "нужно разрешение · " + i.PermissionCmd, StatusNeedsInput
	case i.NeedsInput:
		return "нужно разрешение", StatusNeedsInput
	case i.Interrupted:
		return "прерван — ждёт указаний", StatusNeedsInput
	case i.LimitHit:
		if !i.LimitReset.IsZero() {
			return "упёрся в лимит · продолжу после " + i.LimitReset.Format("15:04"), StatusOnHold
		}
		return "упёрся в лимит", StatusOnHold
	case i.NetworkIssue:
		return "нет сети", StatusNeedsInput
	case i.TrustPrompt:
		return "ждёт тебя · подтверди доверие к папке", StatusNeedsInput
	case i.CallsUserNow():
		return "⚠ НУЖЕН ТЫ · " + i.CallsUser, StatusNeedsInput
	case i.Menu != nil:
		return i.menuLabel()
	case i.WaitReason == "фоновая задача":
		return "в ожидании · " + i.WaitReason, StatusOnHold
	default:
		return i.idleLabel()
	}
}

// lastLines is the last n lines of text.
func lastLines(text string, n int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// isTrustDialog: the bottom of the screen is Claude Code's "trust this folder?"
// question itself (its own options are there), not text that mentions it.
func isTrustDialog(bottom string) bool {
	marker := false
	for _, m := range tmux.TrustMarkers {
		if strings.Contains(bottom, m) {
			marker = true
		}
	}
	if !marker {
		return false
	}
	low := strings.ToLower(bottom)
	return strings.Contains(low, "trust this folder") || strings.Contains(low, "yes, proceed") || strings.Contains(low, "no, exit")
}

var secretRes = []*regexp.Regexp{
	regexp.MustCompile(`/rest/\d+/[A-Za-z0-9]{6,}`),
	regexp.MustCompile(`glpat-[A-Za-z0-9_\-]{8,}`),
	regexp.MustCompile(`(?i)(bearer|token|authorization)[:= ]+[A-Za-z0-9._\-]{12,}`),
}

// redactSecrets hides webhook paths and tokens in text that goes to statuses,
// digests and notifications (a permission prompt can show a curl with a webhook).
func redactSecrets(s string) string {
	for _, re := range secretRes {
		s = re.ReplaceAllString(s, "***")
	}
	return s
}

var callsUserRe = regexp.MustCompile(`^(?:[⏺●]\s*)?(?:[-*•]\s+)?(?:\*\*)?⚠\s*(?:\*\*)?НУЖЕН ТЫ(?:\*\*)?\s*[:—-]?(?:\*\*)?\s*(.*)`)

// callsUser finds «⚠ НУЖЕН ТЫ: <why>» written by the agent itself in its latest
// answer — at the start of one of its own lines — and returns the reason (one
// line, shortened), or "". The user's prompt (which may quote it) and tool
// output (cs ctl list shows other sessions' «⚠ НУЖЕН ТЫ» statuses) do not count.
func callsUser(turn string) string {
	lines := strings.Split(turn, "\n")
	inPrompt := false
	for n := 0; n < len(lines); n++ {
		l := strings.ReplaceAll(lines[n], "\u00a0", " ")
		t := strings.TrimSpace(l)
		if isUserPromptLine(t) {
			inPrompt = true
			continue
		}
		if inPrompt {
			if strings.HasPrefix(l, "  ") && !strings.ContainsAny(t, "⎿⏺●") && t != "" {
				continue
			}
			inPrompt = false
		}
		indent := len(l) - len(strings.TrimLeft(l, " "))
		if indent > 4 || strings.HasPrefix(t, "⎿") {
			continue // tool output
		}
		m := callsUserRe.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		why := strings.TrimSpace(m[1])
		if why == "" && n+1 < len(lines) {
			why = strings.TrimSpace(lines[n+1])
		}
		if r := []rune(why); len(r) > 90 {
			why = string(r[:90]) + "…"
		}
		if why == "" {
			why = "зайди"
		}
		return why
	}
	return ""
}

// CallsUserNow: the conductor/assistant calls the user and the user has not opened it yet.
func (i *Instance) CallsUserNow() bool {
	return i.CallsUser != "" && i.CallsUser != i.CallsUserSeen && i.Status != Running
}

// menuLabel: a worker's menu is the conductor's to answer (cs ctl answer), unless
// it is risky or waited too long; the conductor's and the assistant's menus are the user's.
func (i *Instance) menuLabel() (string, StatusKind) {
	switch {
	case i.Conductor || i.Assistant || !ConductorActive:
		return "ждёт тебя · выбор в меню", StatusWaiting
	case i.MenuEscalated:
		return "ждёт тебя · выбор в меню, дирижёр не ответил", StatusWaiting
	}
	return "меню · ждёт дирижёра", StatusConductor
}

var stageRules = []struct {
	label    string
	keywords []string
}{
	{"ждёт твоего решения", []string{"твоё решение", "твое решение", "ваше решение", "решите", "решаете", "скажите", "выберите", "с тебя", "от тебя", "нужно твоё", "нужно твое"}},
	{"нет сети / VPN", []string{"vpn", "no such host", "connection closed", "не подключ", "нет сети", "недоступ"}},
	{"ждёт одобрения", []string{"одобрен", "утвержд", "апрув", "approve"}},
	{"ждёт мержа", []string{"merge", "мерж", "влить", "вливан", "слить"}},
	{"ждёт ревью", []string{"ревью", "review"}},
	{"всё готово", []string{"всё закрыто", "все закрыто", "завершены"}},
}

// Stage guesses where the task stands from the agent's latest recap
// ("ждёт ревью", "ждёт мержа", ...). Empty when nothing matches.
func (i *Instance) Stage() string {
	// While the agent works, or after it moved on, the last recap is outdated.
	if i.Status == Running || i.RecapStale {
		return ""
	}
	return classifyRecap(i.Recap)
}

// ReplaceInput sends text to the agent, first erasing any unsent draft in its input
// box. If the draft cannot be cleared, nothing is sent and an error is returned, so
// the new message never gets glued to old text.
func (i *Instance) ReplaceInput(text string) error {
	if !i.started || i.tmuxSession == nil {
		return fmt.Errorf("сессия не запущена")
	}
	currentDraft := func() string {
		content, err := i.tmuxSession.CapturePaneContent()
		if err != nil {
			return ""
		}
		return extractDraft(content)
	}
	for attempt := 0; attempt < 2; attempt++ {
		draft := currentDraft()
		if draft == "" {
			break
		}
		if attempt == 0 {
			SaveWipedDraft(i.Title, draft)
		}
		if attempt == 1 {
			_ = i.tmuxSession.SendKeys("\x05") // Ctrl+E: cursor to end of line
		}
		backspaces := utf8.RuneCountInString(draft) + strings.Count(draft, "\n") + 2
		if err := i.tmuxSession.SendKeys(strings.Repeat("\x7f", backspaces)); err != nil {
			return err
		}
		time.Sleep(250 * time.Millisecond)
	}
	if d := currentDraft(); d != "" {
		return fmt.Errorf("не получилось стереть черновик в «%s», сообщение не отправлено — зайди в сессию и очисти поле вручную", i.Title)
	}
	return i.SendPrompt(text)
}

// InputReady reports whether the agent shows its input box (it is ready for a message).
func (i *Instance) InputReady() bool {
	return i.inputVisible
}

// AcceptSuggestion makes Claude Code take its own prompt suggestion (Tab) and send it.
func (i *Instance) AcceptSuggestion() error {
	if !i.started || i.tmuxSession == nil {
		return nil
	}
	if err := i.tmuxSession.SendKeys("\t"); err != nil {
		return err
	}
	time.Sleep(150 * time.Millisecond)
	return i.tmuxSession.TapEnter()
}

// ClassifyRecap guesses the task stage from a recap text.
func ClassifyRecap(recap string) string {
	return classifyRecap(recap)
}

func classifyRecap(recap string) string {
	text := strings.ToLower(recap)
	if text == "" {
		return ""
	}
	// Prefer the "next step" part of the recap when there is one.
	for _, marker := range []string{"следующий шаг", "дальше", "осталось", "next step", "жду "} {
		if idx := strings.LastIndex(text, marker); idx >= 0 {
			if s := matchStage(text[idx:]); s != "" {
				return s
			}
		}
	}
	// Otherwise the end of the recap says where things stand; the beginning
	// usually restates the goal ("цель — довести MR до merge").
	sentences := strings.FieldsFunc(text, func(r rune) bool { return r == '.' || r == '!' || r == '?' })
	for n := len(sentences) - 1; n >= 0 && n >= len(sentences)-2; n-- {
		if s := matchStage(sentences[n]); s != "" {
			return s
		}
	}
	return matchStage(text)
}

func matchStage(text string) string {
	for _, rule := range stageRules {
		for _, k := range rule.keywords {
			if strings.Contains(text, k) {
				return rule.label
			}
		}
	}
	return ""
}

// resumeChoiceMarkers are what Claude Code shows when it asks how to resume a
// large conversation (from a summary or in full).
var resumeChoiceMarkers = []string{"resume from summary", "resume full session", "resume the full session", "summarize and resume"}

// ShowsResumeChoice reports whether the screen asks how to resume a conversation.
func ShowsResumeChoice(raw string) bool {
	text := strings.ToLower(StripANSI(raw))
	for _, m := range resumeChoiceMarkers {
		if strings.Contains(text, m) {
			return true
		}
	}
	return false
}

// AcceptResumeChoice takes Claude Code's default answer (Enter) to the resume
// question, once, and only right after Squad restarted the agent itself.
func (i *Instance) AcceptResumeChoice(content string) bool {
	if i.resumedAt.IsZero() || time.Since(i.resumedAt) > 3*time.Minute || !i.started || i.tmuxSession == nil {
		return false
	}
	if !ShowsResumeChoice(content) {
		return false
	}
	i.resumedAt = time.Time{}
	return i.tmuxSession.TapEnter() == nil
}

// limitPhrases are Claude Code's own wording for a usage limit; weakLimitMarkers
// count only on an API error line (the agent's text about rate limits is not a limit).
var limitPhrases = []string{"usage limit reached", "you've hit your", "you have hit your", "limit will reset", "out of extra usage", "request not allowed", "limit reached ∙", "limit reached ·"}
var weakLimitMarkers = []string{"rate limit", "usage limit", "limit reached", "429"}

var limitResetRe = regexp.MustCompile(`(?i)resets?\s+(?:at\s+)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)

// detectLimit reports whether the current turn ended on a usage/rate limit error
// and, when Claude Code says so, when the limit resets.
func detectLimit(turn string, now time.Time) (bool, time.Time) {
	lines := strings.Split(strings.TrimRight(turn, "\n"), "\n")
	if len(lines) > 15 {
		lines = lines[len(lines)-15:]
	}
	hit := false
	inPrompt := false
	for _, l := range lines {
		t := strings.ToLower(strings.TrimSpace(l))
		if isUserPromptLine(strings.TrimSpace(l)) {
			inPrompt = true // the user's own message (may mention limits) until a blank line
			continue
		}
		if inPrompt {
			// The prompt wraps onto lines indented by two spaces; anything else ends it.
			if strings.HasPrefix(l, "  ") && !strings.ContainsAny(t, "⎿⏺●") && t != "" {
				continue
			}
			inPrompt = false
		}
		for _, m := range limitPhrases {
			if strings.Contains(t, m) {
				hit = true
			}
		}
		if strings.Contains(t, "api error") {
			for _, m := range weakLimitMarkers {
				if strings.Contains(t, m) {
					hit = true
				}
			}
		}
	}
	if !hit {
		return false, time.Time{}
	}
	text := strings.ToLower(strings.Join(lines, "\n"))
	m := limitResetRe.FindStringSubmatch(text)
	if m == nil {
		return true, time.Time{}
	}
	h, _ := strconv.Atoi(m[1])
	min := 0
	if m[2] != "" {
		min, _ = strconv.Atoi(m[2])
	}
	switch strings.ToLower(m[3]) {
	case "pm":
		if h < 12 {
			h += 12
		}
	case "am":
		if h == 12 {
			h = 0
		}
	}
	if h > 23 || min > 59 {
		return true, time.Time{}
	}
	at := time.Date(now.Year(), now.Month(), now.Day(), h, min, 0, 0, now.Location())
	if at.Before(now.Add(-time.Hour)) {
		at = at.Add(24 * time.Hour)
	}
	return true, at
}

// UpdatedAfter reports whether the screen changed (the agent did something) after t.
func (i *Instance) UpdatedAfter(t time.Time) bool { return i.IdleSince.After(t) }
