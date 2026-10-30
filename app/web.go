package app

import (
	"claude-squad/config"
	"claude-squad/log"
	"claude-squad/session"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"claude-squad/ui"
)

// The web interface: the same sessions, «ждут тебя» and live terminals in a browser —
// on the Mac (127.0.0.1) and from the phone over Tailscale (the Mac's 100.x address).
// It runs inside Squad and costs no tokens. Every request needs the key from
// ~/.claude-squad/web-token: a terminal in a browser is a shell on the Mac.

//go:embed web
var webFiles embed.FS

const webTokenFile = "web-token"

// webToken reads (or creates) the key that opens the web interface.
func webToken() (string, error) {
	dir, err := config.GetConfigDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, webTokenFile)
	if data, err := os.ReadFile(path); err == nil {
		if t := strings.TrimSpace(string(data)); len(t) >= 32 {
			return t, nil
		}
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	t := hex.EncodeToString(b)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return t, os.WriteFile(path, []byte(t+"\n"), 0600)
}

// tailscaleAddrs are the Mac's own Tailscale addresses (100.64.0.0/10).
func tailscaleAddrs() []string {
	_, cgnat, _ := net.ParseCIDR("100.64.0.0/10")
	var out []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, ifc := range ifaces {
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && cgnat.Contains(ipn.IP) {
				out = append(out, ipn.IP.String())
			}
		}
	}
	return out
}

// WebURLs are the addresses of the web interface with the key, for `cs web`.
func WebURLs() ([]string, error) {
	cfg := config.LoadConfig()
	if cfg.WebUI == "off" {
		return nil, fmt.Errorf("веб-интерфейс выключен (\"web_ui\": \"off\" в конфиге)")
	}
	tok, err := webToken()
	if err != nil {
		return nil, err
	}
	port := cfg.WebPortOrDefault()
	urls := []string{fmt.Sprintf("http://127.0.0.1:%d/?key=%s", port, tok)}
	for _, a := range tailscaleAddrs() {
		urls = append(urls, fmt.Sprintf("http://%s:%d/?key=%s", a, port, tok))
	}
	return urls, nil
}

// startWeb serves the web interface; it never stops Squad if it cannot listen.
func (m *home) startWeb() {
	if m.appConfig.WebUI == "off" {
		return
	}
	tok, err := webToken()
	if err != nil {
		log.WarningLog.Printf("web: no key: %v", err)
		return
	}
	mux := http.NewServeMux()
	static, _ := fs.Sub(webFiles, "web")
	files := http.FileServer(http.FS(static))
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("X-Squad-Key")
			if key == "" {
				key = r.URL.Query().Get("key")
			}
			if key == "" {
				if c, err := r.Cookie("squad_key"); err == nil {
					key = c.Value
				}
			}
			if subtle.ConstantTimeCompare([]byte(key), []byte(tok)) != 1 {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusUnauthorized)
				fmt.Fprint(w, `<meta name="viewport" content="width=device-width"><body style="font:16px system-ui;background:#16161e;color:#c0caf5;padding:24px">Нужен ключ. Открой ссылку из <code>cs web</code> на маке.</body>`)
				return
			}
			if r.URL.Query().Get("key") != "" {
				http.SetCookie(w, &http.Cookie{Name: "squad_key", Value: tok, Path: "/", HttpOnly: true,
					SameSite: http.SameSiteStrictMode, MaxAge: 90 * 24 * 3600})
			}
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Frame-Options", "DENY")
			next(w, r)
		}
	}
	// State-changing calls only by POST with the key in a header (not a cookie alone):
	// another site open in the same browser cannot send them.
	post := func(next http.HandlerFunc) http.HandlerFunc {
		return auth(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.Header.Get("X-Squad-Key") == "" {
				http.Error(w, "POST с ключом в заголовке", http.StatusMethodNotAllowed)
				return
			}
			next(w, r)
		})
	}
	mux.HandleFunc("/", auth(func(w http.ResponseWriter, r *http.Request) { files.ServeHTTP(w, r) }))
	mux.HandleFunc("/api/key", auth(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"key": tok})
	}))
	mux.HandleFunc("/api/state", auth(m.webState))
	mux.HandleFunc("/api/stream", auth(m.webStream))
	mux.HandleFunc("/api/history", auth(m.webHistory))
	mux.HandleFunc("/api/keys", post(m.webKeys))
	mux.HandleFunc("/api/send", post(m.webSend))
	mux.HandleFunc("/api/answer", post(m.webAnswer))
	mux.HandleFunc("/api/approve", post(m.webApprove))
	mux.HandleFunc("/api/action", post(m.webAction))
	mux.HandleFunc("/api/pet", auth(m.webPet))
	mux.HandleFunc("/api/color", post(m.webColor))
	mux.HandleFunc("/api/group", post(m.webGroup))
	mux.HandleFunc("/api/pet/next", post(m.webPetNext))
	mux.HandleFunc("/api/prefs", auth(webPrefs))
	mux.HandleFunc("/api/events", auth(webEvents))

	port := m.appConfig.WebPortOrDefault()
	addrs := append([]string{"127.0.0.1"}, tailscaleAddrs()...)
	for _, a := range addrs {
		ln, err := net.Listen("tcp", net.JoinHostPort(a, strconv.Itoa(port)))
		if err != nil {
			log.WarningLog.Printf("web: listen %s:%d: %v", a, port, err)
			continue
		}
		srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
		go func() { _ = srv.Serve(ln) }()
		log.InfoLog.Printf("web: http://%s:%d", a, port)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

// webSession is a session as the page shows it.
type webSession struct {
	session.StatusEntry
	Kind       int       `json:"kind"` // session.StatusKind: the color of the status
	Calls      bool      `json:"calls,omitempty"`
	Permission string    `json:"permission,omitempty"`
	Added      int       `json:"added"`
	Removed    int       `json:"removed"`
	Preview    []string  `json:"preview,omitempty"` // the last lines of its screen
	Started    bool      `json:"started"`
	Paused     bool      `json:"paused,omitempty"`
	ActiveAt   time.Time `json:"active_at,omitempty"` // the screen last changed
}

type webQueueItem struct {
	Number int                 `json:"number"`
	Title  string              `json:"title"`
	Kind   string              `json:"kind"`
	Text   string              `json:"text"`
	Since  time.Time           `json:"since,omitempty"`
	Menu   *session.ChoiceMenu `json:"menu,omitempty"`
	Answer bool                `json:"answerable,omitempty"`
	Perm   bool                `json:"permission,omitempty"`
}

func (m *home) webState(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	var out struct {
		Version  string            `json:"version"`
		Sessions []webSession      `json:"sessions"`
		Queue    []webQueueItem    `json:"queue"`
		Banner   string            `json:"banner,omitempty"`
		Disk     float64           `json:"disk_gb"`
		VPNDown  bool              `json:"vpn_down,omitempty"`
		At       time.Time         `json:"at"`
		Capacity *session.Capacity `json:"capacity,omitempty"`
		Usage5h  float64           `json:"usage_5h,omitempty"` // % of the configured budget
		UsageWk  float64           `json:"usage_week,omitempty"`
		Units5h  float64           `json:"units_5h,omitempty"`
		UnitsWk  float64           `json:"units_week,omitempty"`
		Budgeted bool              `json:"budgeted,omitempty"`
		Real     bool              `json:"usage_real,omitempty"` // the numbers are Anthropic's, as /usage
		Reset5h  time.Time         `json:"reset_5h,omitempty"`
		ResetWk  time.Time         `json:"reset_week,omitempty"`
		RealErr  string            `json:"usage_err,omitempty"`
	}
	out.Version, out.At = Version, time.Now()
	instances := m.list.GetInstances()
	for _, inst := range instances {
		if !inst.Started() && !inst.Paused() {
			continue
		}
		_, kind := inst.StatusLabel()
		ws := webSession{StatusEntry: inst.SnapshotEntry(), Kind: int(kind), Calls: inst.CallsUserNow(),
			Permission: inst.PermissionCmd, Started: inst.Started(), Paused: inst.Paused(), ActiveAt: inst.ActiveAt}
		if st := inst.GetDiffStats(); st != nil && st.Error == nil {
			ws.Added, ws.Removed = st.Added, st.Removed
		}
		if inst.Started() && !inst.Paused() {
			ws.Preview = screenPreview(inst.Screen(), 10)
		}
		out.Sessions = append(out.Sessions, ws)
	}
	for _, it := range session.UserQueue(instances) {
		out.Queue = append(out.Queue, webQueueItem{Number: it.Inst.Number, Title: it.Inst.Title, Kind: it.KindLabel(),
			Text: it.Text, Since: it.Since, Menu: it.Inst.Menu, Answer: it.Answerable(), Perm: it.Kind == session.QueuePermission})
	}
	out.Banner = m.usageBanner()
	out.VPNDown = m.vpnDown()
	out.Usage5h, out.UsageWk, out.Units5h, out.UnitsWk = m.usage.pct5h, m.usage.pctWk, m.usage.units5h, m.usage.unitsWk
	out.Budgeted = m.appConfig.TokenBudget5h > 0 || m.appConfig.TokenBudgetWeek > 0
	if m.usage.planFresh() {
		out.Real, out.Reset5h, out.ResetWk = true, m.usage.plan.FiveReset, m.usage.plan.WeekReset
	}
	out.RealErr = m.usage.planErr
	snap := &session.StatusSnapshot{Usage5hPct: m.usage.pct5h, UsageWeekPct: m.usage.pctWk, VPNDown: out.VPNDown}
	for _, inst := range instances {
		if inst.Started() {
			snap.Sessions = append(snap.Sessions, inst.SnapshotEntry())
		}
	}
	maxActive := m.appConfig.MaxActive()
	m.mu.Unlock()
	c := session.ComputeCapacity(snap, maxActive)
	out.Capacity = &c
	out.Disk = -1
	if home, err := os.UserHomeDir(); err == nil {
		out.Disk = session.DiskFreeGB(home)
	}
	writeJSON(w, out)
}

// findByNumber returns the session with this number (caller holds m.mu).
func (m *home) findByNumber(n int) *session.Instance {
	for _, inst := range m.list.GetInstances() {
		if inst.Number == n {
			return inst
		}
	}
	return nil
}

func formInt(r *http.Request, name string) int {
	n, _ := strconv.Atoi(r.FormValue(name))
	return n
}

// paneFrame is one screen of a session for the browser terminal.
type paneFrame struct {
	HTML string `json:"html"` // the screen as HTML spans, cursor marked
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

func capturePane(name string) (paneFrame, error) {
	var f paneFrame
	out, err := exec.Command("tmux", "display-message", "-p", "-t", "="+name+":",
		"#{pane_width} #{pane_height} #{cursor_x} #{cursor_y} #{cursor_flag}").Output()
	if err != nil {
		return f, err
	}
	var cx, cy, shown int
	fmt.Sscanf(strings.TrimSpace(string(out)), "%d %d %d %d %d", &f.Cols, &f.Rows, &cx, &cy, &shown)
	scr, err := exec.Command("tmux", "capture-pane", "-p", "-e", "-t", "="+name+":").Output()
	if err != nil {
		return f, err
	}
	lines := strings.Split(strings.TrimRight(string(scr), "\n"), "\n")
	if f.Rows > 0 && len(lines) > f.Rows {
		lines = lines[len(lines)-f.Rows:]
	}
	if shown == 0 {
		cx = -1
	}
	f.HTML = ansiToHTML(lines, cx, cy)
	return f, nil
}

// webHistory returns the scrollback above the visible screen: the last `lines` lines
// that scrolled off it (oldest first), as HTML — the page shows it above the live
// screen when the user scrolls up.
func (m *home) webHistory(w http.ResponseWriter, r *http.Request) {
	n, lines := formInt(r, "n"), formInt(r, "lines")
	if lines <= 0 {
		lines = 600
	}
	if lines > 20000 {
		lines = 20000
	}
	m.mu.Lock()
	inst := m.findByNumber(n)
	name, conv := "", ""
	if inst != nil {
		e := inst.SnapshotEntry()
		name, conv = e.TmuxName, e.ConvID
	}
	m.mu.Unlock()
	// The conversation from Claude Code's transcript: complete and readable, unlike the
	// terminal's scrollback, which redraws and resizes cut.
	if p := transcriptPath(conv); p != "" {
		if tl := transcriptLines(p); len(tl) > 0 {
			more := len(tl) > lines
			if more {
				tl = tl[len(tl)-lines:]
			}
			writeJSON(w, map[string]any{"html": transcriptHTML(tl), "lines": len(tl), "more": more, "kind": "transcript"})
			return
		}
	}
	if name == "" {
		http.Error(w, "сессия не найдена или спит", http.StatusNotFound)
		return
	}
	out, err := exec.Command("tmux", "capture-pane", "-p", "-e", "-S", "-"+strconv.Itoa(lines), "-E", "-1", "-t", "="+name+":").Output()
	if err != nil {
		http.Error(w, "история недоступна", http.StatusInternalServerError)
		return
	}
	text := strings.TrimRight(string(out), "\n")
	var hl []string
	if text != "" {
		hl = strings.Split(text, "\n")
	}
	var hsize int
	if o, err := exec.Command("tmux", "display-message", "-p", "-t", "="+name+":", "#{history_size}").Output(); err == nil {
		hsize, _ = strconv.Atoi(strings.TrimSpace(string(o)))
	}
	writeJSON(w, map[string]any{"html": ansiToHTML(hl, -1, 0), "lines": len(hl), "more": hsize > lines, "kind": "screen"})
}

// screenKick wakes the stream of a session right after keys were typed into it, so
// the echo shows at once instead of on the next 200 ms tick.
var screenKick = struct {
	sync.Mutex
	m map[string][]chan struct{}
}{m: map[string][]chan struct{}{}}

func kickScreen(name string) {
	screenKick.Lock()
	for _, ch := range screenKick.m[name] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	screenKick.Unlock()
}

func watchKicks(name string) (chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	screenKick.Lock()
	screenKick.m[name] = append(screenKick.m[name], ch)
	screenKick.Unlock()
	return ch, func() {
		screenKick.Lock()
		list := screenKick.m[name]
		for i, c := range list {
			if c == ch {
				screenKick.m[name] = append(list[:i], list[i+1:]...)
				break
			}
		}
		screenKick.Unlock()
	}
}

// webStream sends the session's screen as server-sent events whenever it changes.
func (m *home) webStream(w http.ResponseWriter, r *http.Request) {
	go fitBackgroundWindow()
	n := formInt(r, "n")
	m.mu.Lock()
	inst := m.findByNumber(n)
	name := ""
	if inst != nil {
		name = inst.SnapshotEntry().TmuxName
	}
	m.mu.Unlock()
	if name == "" {
		http.Error(w, "сессия не найдена или спит", http.StatusNotFound)
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no stream", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	last := ""
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	kick, unwatch := watchKicks(name)
	defer unwatch()
	ping := time.Now()
	fast := 0 // quick re-captures after typing: the agent redraws a moment later
	for {
		f, err := capturePane(name)
		if err != nil {
			fmt.Fprintf(w, "event: gone\ndata: {}\n\n")
			fl.Flush()
			return
		}
		data, _ := json.Marshal(f)
		if s := string(data); s != last {
			last = s
			fmt.Fprintf(w, "data: %s\n\n", data)
			fl.Flush()
			ping = time.Now()
		} else if time.Since(ping) > 15*time.Second {
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
			ping = time.Now()
		}
		if fast > 0 {
			fast--
			select {
			case <-r.Context().Done():
				return
			case <-time.After(40 * time.Millisecond):
			}
			continue
		}
		select {
		case <-r.Context().Done():
			return
		case <-kick:
			fast = 5
			time.Sleep(15 * time.Millisecond)
		case <-tick.C:
		}
	}
}

// webKeys types raw keys from the browser terminal into the session.
func (m *home) webKeys(w http.ResponseWriter, r *http.Request) {
	n := formInt(r, "n")
	data := r.FormValue("data")
	if data == "" || len(data) > 8192 {
		http.Error(w, "пусто или слишком длинно", http.StatusBadRequest)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	inst := m.findByNumber(n)
	if inst == nil || !inst.Started() || inst.Paused() {
		http.Error(w, "сессия не найдена или спит", http.StatusNotFound)
		return
	}
	if err := inst.PressKeys(data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	kickScreen(inst.SnapshotEntry().TmuxName)
	writeJSON(w, map[string]bool{"ok": true})
}

// webSend sends a message to the session (typed and submitted, as from the TUI).
func (m *home) webSend(w http.ResponseWriter, r *http.Request) {
	n := formInt(r, "n")
	text := strings.TrimSpace(r.FormValue("text"))
	if text == "" {
		http.Error(w, "пустое сообщение", http.StatusBadRequest)
		return
	}
	m.mu.Lock()
	inst := m.findByNumber(n)
	m.mu.Unlock()
	if inst == nil || !inst.Started() || inst.Paused() {
		http.Error(w, "сессия не найдена или спит — разбуди её", http.StatusNotFound)
		return
	}
	// SendPrompt waits for the text to appear: do not hold the lock meanwhile.
	if err := inst.SendPrompt(text); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// webAnswer picks an option of a session's choice menu.
func (m *home) webAnswer(w http.ResponseWriter, r *http.Request) {
	n, opt := formInt(r, "n"), formInt(r, "option")
	m.mu.Lock()
	defer m.mu.Unlock()
	inst := m.findByNumber(n)
	if inst == nil || inst.Menu == nil {
		http.Error(w, "меню уже закрыто", http.StatusConflict)
		return
	}
	o, ok := inst.Menu.Option(opt)
	if !ok || o.Free || o.Chat || inst.Menu.Multi || inst.Menu.Review {
		http.Error(w, "этот вариант выбери в терминале сессии", http.StatusBadRequest)
		return
	}
	if err := inst.PressKeys(strconv.Itoa(opt)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	inst.MenuHandled = session.MenuKey(*inst.Menu)
	session.AppendEvent(session.Event{Kind: "user", Number: inst.Number, Title: inst.Title,
		Text: fmt.Sprintf("#%d: ты выбрал «%s» (веб)", inst.Number, o.Label)})
	writeJSON(w, map[string]bool{"ok": true})
}

// webApprove allows a session's permission request once ("1. Yes").
func (m *home) webApprove(w http.ResponseWriter, r *http.Request) {
	n := formInt(r, "n")
	m.mu.Lock()
	defer m.mu.Unlock()
	inst := m.findByNumber(n)
	if inst == nil || !inst.NeedsInput || inst.Menu != nil {
		http.Error(w, "запроса разрешения уже нет", http.StatusConflict)
		return
	}
	if err := inst.PressKeys("1"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	session.AppendEvent(session.Event{Kind: "user", Number: inst.Number, Title: inst.Title,
		Text: fmt.Sprintf("#%d: ты разрешил один раз (веб) — %s", inst.Number, inst.PermissionCmd)})
	writeJSON(w, map[string]bool{"ok": true})
}

// webAction wakes or restarts a session (the same queue as cs ctl).
func (m *home) webAction(w http.ResponseWriter, r *http.Request) {
	n := formInt(r, "n")
	act := r.FormValue("action")
	m.mu.Lock()
	inst := m.findByNumber(n)
	title := ""
	if inst != nil {
		title = inst.Title
	}
	m.mu.Unlock()
	if title == "" {
		http.Error(w, "сессия не найдена", http.StatusNotFound)
		return
	}
	req := session.SessionRequest{Title: title}
	switch act {
	case "wake":
		req.Wake = true
	case "restart":
		req.Restart = true
	default:
		http.Error(w, "действие: wake или restart", http.StatusBadRequest)
		return
	}
	if _, err := session.SubmitSessionRequest(req); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// screenPreview is the last n meaningful lines of a screen: no colors, no empty
// lines, no input box frame (───, the ❯ prompt line), each cut to 200 characters.
func screenPreview(raw string, n int) []string {
	lines := strings.Split(session.StripANSI(raw), "\n")
	// Drop the input box at the bottom: everything from the last frame line above it.
	cut := len(lines)
	seen := 0
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-12; i-- {
		t := strings.TrimSpace(lines[i])
		if t != "" && strings.Trim(t, "─━-") == "" {
			seen++
			cut = i
			if seen == 2 {
				break
			}
		}
	}
	var out []string
	for i := cut - 1; i >= 0 && len(out) < n; i-- {
		t := strings.TrimRight(lines[i], " ")
		if strings.TrimSpace(t) == "" || strings.Trim(strings.TrimSpace(t), "─━-") == "" {
			continue
		}
		if r := []rune(t); len(r) > 200 {
			t = string(r[:200]) + "…"
		}
		out = append([]string{t}, out...)
	}
	return out
}

// webPet is the pet(s) with their frames, phrase and density caption.
func (m *home) webPet(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	cfg := m.petConfig()
	m.mu.Unlock()
	writeJSON(w, map[string]any{"pet": ui.PetsForWeb(cfg, time.Now()), "kind": cfg.Kind, "name": ui.PetName(cfg.Kind)})
}

// webPetNext switches to the next pet, like p in the overview.
func (m *home) webPetNext(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	m.appConfig.Pet = ui.NextPet(m.appConfig.Pet, m.appConfig.PetDir != "")
	err := config.SaveConfig(m.appConfig)
	kind := m.appConfig.Pet
	m.mu.Unlock()
	if err != nil {
		log.WarningLog.Printf("save pet: %v", err)
	}
	writeJSON(w, map[string]string{"kind": kind, "name": ui.PetName(kind)})
}

// webColor switches the session's color label to the next one (like C in Squad).
func (m *home) webColor(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	inst := m.findByNumber(formInt(r, "n"))
	if inst == nil {
		http.Error(w, "сессия не найдена", http.StatusNotFound)
		return
	}
	inst.CycleColor()
	_ = m.storage.SaveInstances(m.list.GetInstances())
	writeJSON(w, map[string]string{"color": inst.Color})
}

// webGroup sets the session's group: qa, dev or auto.
func (m *home) webGroup(w http.ResponseWriter, r *http.Request) {
	g, ok := session.NormGroup(r.FormValue("group"))
	if !ok {
		http.Error(w, "группа: qa, dev, auto или своё название до 32 символов", http.StatusBadRequest)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	inst := m.findByNumber(formInt(r, "n"))
	if inst == nil || inst.Conductor || inst.Assistant {
		http.Error(w, "сессия не найдена", http.StatusNotFound)
		return
	}
	inst.Group = g
	_ = m.storage.SaveInstances(m.list.GetInstances())
	writeJSON(w, map[string]string{"group": inst.GroupOf()})
}

// Squad in the background runs in the tmux session "orchestra" (orchestra-start), and
// its window decides how wide every session is drawn. Attaching from a small terminal
// shrinks that window and it stays small after detaching, so the web view gets narrow
// sessions. While nobody is attached, the web view puts the window back to full size.
const bgWinW, bgWinH = 240, 64

var fitMu sync.Mutex
var fitLast time.Time

func fitBackgroundWindow() {
	pane := os.Getenv("TMUX_PANE")
	if os.Getenv("TMUX") == "" || pane == "" {
		return
	}
	fitMu.Lock()
	if time.Since(fitLast) < 5*time.Second {
		fitMu.Unlock()
		return
	}
	fitLast = time.Now()
	fitMu.Unlock()
	out, err := exec.Command("tmux", "display-message", "-p", "-t", pane,
		"#{session_name} #{session_attached} #{window_width} #{window_height}").Output()
	if err != nil {
		return
	}
	var name string
	var attached, w, h int
	if _, err := fmt.Sscan(strings.TrimSpace(string(out)), &name, &attached, &w, &h); err != nil {
		return
	}
	if name != "orchestra" || attached != 0 || (w >= bgWinW && h >= bgWinH) {
		return
	}
	if err := exec.Command("tmux", "resize-window", "-t", pane, "-x", strconv.Itoa(bgWinW), "-y", strconv.Itoa(bgWinH)).Run(); err != nil {
		log.WarningLog.Printf("web: could not resize the background window: %v", err)
		return
	}
	log.InfoLog.Printf("web: background window %dx%d -> %dx%d", w, h, bgWinW, bgWinH)
}

// The page's own settings (layout, pins, filters, notes) live in web-prefs.json, so
// the app window and a browser show the same layout. The page owns the format.
const webPrefsMax = 256 << 10

func webPrefsPath() (string, error) {
	dir, err := config.GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "web-prefs.json"), nil
}

var prefsMu sync.Mutex

func webPrefs(w http.ResponseWriter, r *http.Request) {
	path, err := webPrefsPath()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	prefsMu.Lock()
	defer prefsMu.Unlock()
	if r.Method != http.MethodPost {
		data, err := os.ReadFile(path)
		if err != nil || !json.Valid(data) {
			data = []byte("{}")
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(data)
		return
	}
	if r.Header.Get("X-Squad-Key") == "" {
		http.Error(w, "POST с ключом в заголовке", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, webPrefsMax+4096)
	data := []byte(r.FormValue("data"))
	var probe map[string]any
	if len(data) > webPrefsMax || json.Unmarshal(data, &probe) != nil {
		http.Error(w, "настройки: нужен JSON-объект до 256 КБ", http.StatusBadRequest)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil || os.Rename(tmp, path) != nil {
		http.Error(w, "не удалось сохранить настройки", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// webEvents returns the last events (status changes, answers, reports) for the feed.
func webEvents(w http.ResponseWriter, r *http.Request) {
	hours := formInt(r, "hours")
	if hours <= 0 || hours > 72 {
		hours = 24
	}
	ev := session.ReadEvents(time.Now().Add(-time.Duration(hours) * time.Hour))
	if len(ev) > 400 {
		ev = ev[len(ev)-400:]
	}
	if ev == nil {
		ev = []session.Event{}
	}
	writeJSON(w, ev)
}
