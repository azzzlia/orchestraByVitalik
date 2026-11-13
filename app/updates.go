package app

import (
	"bytes"
	"claude-squad/config"
	"claude-squad/log"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Разбор обновлений в тестировании. Обновление модуля — ветка update/<модуль>/<версия>,
// срезанная с master; когда оно выходит, ветка становится closed-update/… Squad сама
// (без токенов) видит открытые ветки и читает чат, где объявляют «забрать в тест»,
// а дайджест строит только по кнопке: цифры считает git, текст пишет модель один раз
// на обновление; «Дополнить» отдаёт модели только коммиты новее прошлого разбора.

const (
	updScanEvery   = 30 * time.Minute
	updModelLimit  = 12 * time.Minute
	updDefaultChat = ""
)

type updEntry struct {
	Module   string `json:"module"`
	Version  string `json:"version"`
	Head     string `json:"head,omitempty"`   // голова ветки при последнем скане
	Closed   bool   `json:"closed,omitempty"` // уже closed-update — обновление вышло
	Gone     bool   `json:"gone,omitempty"`   // ветки нет (добавили руками, а её нет)
	Manual   bool   `json:"manual,omitempty"`
	InTest   bool   `json:"in_test,omitempty"` // объявлено в чате
	TestNote string `json:"test_note,omitempty"`
	TestAt   string `json:"test_at,omitempty"`
	DoneHead string `json:"done_head,omitempty"` // голова ветки на момент разбора
	DoneAt   string `json:"done_at,omitempty"`
	Status   string `json:"status,omitempty"` // "" | queued | running | error
	Err      string `json:"err,omitempty"`
	New      int    `json:"new,omitempty"` // коммитов в ветке после разбора
}

func (e *updEntry) key() string { return e.Module + "/" + e.Version }

type updater struct {
	mu      sync.Mutex
	repo    string
	modules []string
	chat    string
	model   string
	program string
	dir     string
	E       map[string]*updEntry `json:"entries"`
	ChatErr string               `json:"chat_err,omitempty"`
	ScanAt  string               `json:"scan_at,omitempty"`
	queue   chan string
	scanMu  sync.Mutex
	lastRun time.Time
}

func newUpdater(cfg *config.Config) *updater {
	u := &updater{E: map[string]*updEntry{}, queue: make(chan string, 64), model: "sonnet", program: "claude", chat: updDefaultChat}
	home, _ := os.UserHomeDir()
	u.repo = filepath.Join(home, "devRepo", "modules")
	u.modules = []string{"im"}
	if cfg != nil {
		if cfg.UpdatesRepo != "" {
			u.repo = expandHome(cfg.UpdatesRepo)
		}
		if len(cfg.UpdatesModules) > 0 {
			u.modules = cfg.UpdatesModules
		}
		if cfg.UpdatesChat != "" {
			u.chat = cfg.UpdatesChat
		}
		if cfg.UpdatesModel != "" {
			u.model = cfg.UpdatesModel
		}
	}
	if d, err := config.GetConfigDir(); err == nil {
		u.dir = filepath.Join(d, "updates")
		_ = os.MkdirAll(u.dir, 0o755)
	}
	if _, err := exec.LookPath("claude"); err != nil {
		for _, p := range []string{filepath.Join(home, ".local", "bin", "claude"), filepath.Join(home, ".claude", "local", "claude"), "/opt/homebrew/bin/claude", "/usr/local/bin/claude"} {
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				u.program = p
				break
			}
		}
	}
	u.load()
	for _, e := range u.E { // разбор, прерванный перезапуском, — заново в очередь не ставим
		if e.Status == "running" || e.Status == "queued" {
			e.Status, e.Err = "error", "прервано перезапуском Squad — нажми ещё раз"
		}
	}
	return u
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		h, _ := os.UserHomeDir()
		return filepath.Join(h, p[2:])
	}
	return p
}

func (u *updater) statePath() string { return filepath.Join(u.dir, "state.json") }

func (u *updater) load() {
	b, err := os.ReadFile(u.statePath())
	if err != nil {
		return
	}
	_ = json.Unmarshal(b, u)
	if u.E == nil {
		u.E = map[string]*updEntry{}
	}
}

// save: под u.mu.
func (u *updater) save() {
	if u.dir == "" {
		return
	}
	b, _ := json.MarshalIndent(u, "", " ")
	tmp := u.statePath() + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, u.statePath())
	}
}

// run: фоновый скан раз в полчаса и очередь разборов (по одному за раз).
func (u *updater) run() {
	go func() {
		time.Sleep(2 * time.Minute)
		for {
			u.scan(true)
			time.Sleep(updScanEvery)
		}
	}()
	go func() {
		for k := range u.queue {
			u.digest(k)
		}
	}()
}

func (u *updater) git(timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	c := exec.CommandContext(ctx, "git", append([]string{"-C", u.repo}, args...)...)
	var errb bytes.Buffer
	c.Stderr = &errb
	out, err := c.Output()
	if err != nil {
		msg := strings.TrimSpace(errb.String())
		if i := strings.LastIndex(msg, "\n"); i >= 0 {
			msg = msg[i+1:]
		}
		return string(out), fmt.Errorf("git %s: %v %s", args[0], err, msg)
	}
	return string(out), nil
}

var updVerRe = regexp.MustCompile(`^\d+(\.\d+)+$`)

func verLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x < y
		}
	}
	return false
}

type updRef struct {
	ver, head string
	closed    bool
}

// refs: ветки обновлений модуля (open и closed), по версии.
func (u *updater) refs(mod string) ([]updRef, error) {
	out, err := u.git(30*time.Second, "for-each-ref", "--format=%(refname) %(objectname)",
		"refs/remotes/origin/update/"+mod+"/", "refs/remotes/origin/closed-update/"+mod+"/")
	if err != nil {
		return nil, err
	}
	var rs []updRef
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Fields(l)
		if len(f) != 2 {
			continue
		}
		v := f[0][strings.LastIndex(f[0], "/")+1:]
		if !updVerRe.MatchString(v) {
			continue
		}
		rs = append(rs, updRef{ver: v, head: f[1], closed: strings.Contains(f[0], "/closed-update/")})
	}
	sort.Slice(rs, func(i, j int) bool { return verLess(rs[i].ver, rs[j].ver) })
	return rs, nil
}

func branchOf(mod, ver string, closed bool) string {
	if closed {
		return "origin/closed-update/" + mod + "/" + ver
	}
	return "origin/update/" + mod + "/" + ver
}

// scan: свежие ветки обновлений (git fetch только нужных веток) и объявления в чате.
func (u *updater) scan(fetch bool) {
	if !u.scanMu.TryLock() {
		return
	}
	defer u.scanMu.Unlock()
	if fetch {
		specs := []string{"fetch", "--quiet", "--no-tags", "origin", "+refs/heads/master:refs/remotes/origin/master"}
		for _, m := range u.modules {
			specs = append(specs, "+refs/heads/update/"+m+"/*:refs/remotes/origin/update/"+m+"/*",
				"+refs/heads/closed-update/"+m+"/*:refs/remotes/origin/closed-update/"+m+"/*")
		}
		if _, err := u.git(3*time.Minute, specs...); err != nil {
			log.WarningLog.Printf("updates: %v", err)
		}
	}
	type found struct{ refs []updRef }
	all := map[string]found{}
	for _, m := range u.modules {
		rs, err := u.refs(m)
		if err != nil {
			log.WarningLog.Printf("updates: %v", err)
			continue
		}
		all[m] = found{rs}
	}
	msgs, chatErr := u.chatMessages()

	u.mu.Lock()
	defer u.mu.Unlock()
	u.ChatErr = chatErr
	u.ScanAt = time.Now().Format("02.01 15:04")
	seen := map[string]bool{}
	for m, f := range all {
		for _, r := range f.refs {
			k := m + "/" + r.ver
			e := u.E[k]
			if e == nil {
				if r.closed && u.E[k] == nil {
					continue // вышедшие обновления не заводим
				}
				e = &updEntry{Module: m, Version: r.ver}
				u.E[k] = e
			}
			seen[k] = true
			e.Gone = false
			if r.closed {
				e.Closed, e.InTest = true, false
			}
			e.Head = r.head
		}
	}
	for k, e := range u.E {
		if !seen[k] {
			e.Gone = true
		}
	}
	u.applyChat(msgs)
	for _, e := range u.E {
		e.New = 0
		if e.DoneHead != "" && e.Head != "" && e.Head != e.DoneHead {
			if out, err := u.git(20*time.Second, "rev-list", "--count", e.DoneHead+".."+e.Head, "--", e.Module+"/"); err == nil {
				e.New, _ = strconv.Atoi(strings.TrimSpace(out))
			}
		}
	}
	u.save()
}

type chatMsg struct {
	at   time.Time
	text string
}

// webhook: тот же вебхук Б24, что у помощника (только чтение).
func (u *updater) webhook() string {
	b, err := os.ReadFile(filepath.Join(u.repo, ".ai", "local-settings.yaml"))
	if err == nil {
		if m := regexp.MustCompile(`(?m)^\s*tracker_webhook:\s*['"]?([^'"\n#]+)`).FindSubmatch(b); m != nil {
			return strings.TrimRight(strings.TrimSpace(string(m[1])), "/")
		}
	}
	return strings.TrimRight(os.Getenv("TRACKER_WEBHOOK"), "/")
}

var updChatMsgs = func(u *updater) ([]chatMsg, string) {
	if u.chat == "" || u.chat == "off" {
		return nil, ""
	}
	wh := u.webhook()
	if wh == "" {
		return nil, "нет вебхука Б24 (tracker_webhook в .ai/local-settings.yaml)"
	}
	form := url.Values{"DIALOG_ID": {u.chat}, "LIMIT": {"50"}}
	cl := &http.Client{Timeout: 25 * time.Second}
	resp, err := cl.PostForm(wh+"/im.dialog.messages.get.json", form)
	if err != nil {
		return nil, "чат не прочитан: нет связи с Б24 (VPN?)"
	}
	defer resp.Body.Close()
	var r struct {
		Result struct {
			Messages []struct {
				Date string `json:"date"`
				Text string `json:"text"`
			} `json:"messages"`
		} `json:"result"`
		Error     string `json:"error"`
		ErrorDesc string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, "чат не прочитан: непонятный ответ Б24"
	}
	if r.Error != "" {
		return nil, "чат не прочитан: " + r.Error + " " + r.ErrorDesc
	}
	var out []chatMsg
	for _, m := range r.Result.Messages {
		t, _ := time.Parse(time.RFC3339, m.Date)
		out = append(out, chatMsg{at: t, text: m.Text})
	}
	return out, ""
}

func (u *updater) chatMessages() ([]chatMsg, string) { return updChatMsgs(u) }

var updBB = regexp.MustCompile(`\[/?[a-zA-Z]+[^\]]*\]`)

// applyChat: «… im 26.1700.0 … тест …» — обновление в тесте. Под u.mu.
func (u *updater) applyChat(msgs []chatMsg) {
	if len(msgs) == 0 || len(u.modules) == 0 {
		return
	}
	qm := make([]string, len(u.modules))
	for i, m := range u.modules {
		qm[i] = regexp.QuoteMeta(m)
	}
	re := regexp.MustCompile(`(?i)(?:^|[^a-z0-9_])(` + strings.Join(qm, "|") + `)[\s:/_-]+(\d{2}\.\d{2,5}\.\d+)`)
	sort.Slice(msgs, func(i, j int) bool { return msgs[i].at.Before(msgs[j].at) })
	for _, m := range msgs {
		text := strings.Join(strings.Fields(updBB.ReplaceAllString(m.text, "")), " ")
		if !strings.Contains(strings.ToLower(text), "тест") {
			continue
		}
		for _, g := range re.FindAllStringSubmatch(text, -1) {
			k := strings.ToLower(g[1]) + "/" + g[2]
			e := u.E[k]
			if e == nil || e.Closed {
				continue
			}
			e.InTest = true
			e.TestNote = clipRunes(text, 160)
			if !m.at.IsZero() {
				e.TestAt = m.at.Local().Format("02.01 15:04")
			}
		}
	}
}

func clipRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// ---------- разбор ----------

type updItem struct {
	kind     string // mr | direct | cut (в ветку после среза)
	hash     string
	subj     string
	files    int
	areas    []string
	testOnly bool
}

var (
	updTestRe = regexp.MustCompile(`(\.spec\.|\.test\.|/tests?/|/__tests__/)`)
	updDocRe  = regexp.MustCompile(`(^\.ai/|\.md$|/docs/|knowledge)`)
	updJsRe   = regexp.MustCompile(`^install/js/[^/]+/v2/(component|lib|application|provider|model|const|css)/([^/]+)`)
	updJs1Re  = regexp.MustCompile(`^install/js/[^/]+/([^/]+)`)
	updPhpRe  = regexp.MustCompile(`^lib/V2/([^/.]+)`)
	updPhp1Re = regexp.MustCompile(`^lib/([^/.]+)`)
)

func updArea(mod, f string) string {
	p := strings.SplitN(f, "/", 2)
	if len(p) < 2 || p[0] != mod {
		return "~" + p[0]
	}
	s := p[1]
	switch {
	case strings.HasPrefix(s, "tests/") || updTestRe.MatchString(s):
		return "#тесты"
	case strings.Contains(s, "/dist/"):
		return "#dist"
	case updDocRe.MatchString(s):
		return "#docs"
	case strings.HasPrefix(s, "dev/updates/") || strings.HasPrefix(s, "install/migrations/"):
		return "апдейтер"
	case strings.Contains(s, "/lang/") || strings.HasPrefix(s, "lang/"):
		return "#переводы"
	}
	if m := updJsRe.FindStringSubmatch(s); m != nil {
		return "js:" + m[1] + "/" + m[2]
	}
	if m := updJs1Re.FindStringSubmatch(s); m != nil {
		return "js:" + m[1]
	}
	if m := updPhpRe.FindStringSubmatch(s); m != nil {
		return "php:V2/" + m[1]
	}
	if m := updPhp1Re.FindStringSubmatch(s); m != nil {
		return "php:" + m[1]
	}
	if i := strings.Index(s, "/"); i > 0 {
		return s[:i]
	}
	return s
}

func (u *updater) mkItem(mod, kind, hash, subj string, files []string) updItem {
	cnt := map[string]int{}
	n := 0
	for _, f := range files {
		if f = strings.TrimSpace(f); f == "" {
			continue
		}
		n++
		cnt[updArea(mod, f)]++
	}
	it := updItem{kind: kind, hash: hash[:min(10, len(hash))], subj: subj, files: n, testOnly: true}
	type kv struct {
		k string
		v int
	}
	var kvs []kv
	for k, v := range cnt {
		kvs = append(kvs, kv{k, v})
		if !strings.HasPrefix(k, "#") && !strings.HasPrefix(k, "~") {
			it.testOnly = false
		}
	}
	sort.Slice(kvs, func(i, j int) bool { return kvs[i].v > kvs[j].v || kvs[i].v == kvs[j].v && kvs[i].k < kvs[j].k })
	for i, x := range kvs {
		if i == 4 {
			break
		}
		it.areas = append(it.areas, x.k)
	}
	return it
}

// items: что пришло в ветку от base до head. Из master — MR (первый родитель) и прямые коммиты,
// из самой ветки — то, что долили после среза.
func (u *updater) items(mod, mFrom, mTo, bFrom, bTo string) ([]updItem, error) {
	var res []updItem
	add := func(kind string, args ...string) error {
		out, err := u.git(2*time.Minute, args...)
		if err != nil {
			return err
		}
		for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
			f := strings.SplitN(l, "\t", 2)
			if len(f) != 2 {
				continue
			}
			var fl string
			if kind == "direct" {
				fl, err = u.git(time.Minute, "show", "--name-only", "--format=", f[0], "--", mod+"/")
			} else {
				fl, err = u.git(time.Minute, "diff", "--name-only", f[0]+"^1", f[0], "--", mod+"/")
			}
			if err != nil {
				continue
			}
			res = append(res, u.mkItem(mod, kind, f[0], f[1], strings.Split(fl, "\n")))
		}
		return nil
	}
	if mFrom != "" && mFrom != mTo {
		if err := add("mr", "log", "--merges", "--first-parent", "--format=%H\t%s", mFrom+".."+mTo, "--", mod+"/"); err != nil {
			return nil, err
		}
		if err := add("direct", "log", "--no-merges", "--first-parent", "--format=%H\t%s", mFrom+".."+mTo, "--", mod+"/"); err != nil {
			return nil, err
		}
	}
	if bFrom != bTo {
		// в ветке обновления: мержи — как MR, отдельные коммиты — как прямые; первый родитель
		out, err := u.git(2*time.Minute, "log", "--first-parent", "--format=%H\t%P\t%s", bFrom+".."+bTo, "--", mod+"/")
		if err != nil {
			return nil, err
		}
		for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
			f := strings.SplitN(l, "\t", 3)
			if len(f) != 3 {
				continue
			}
			var fl string
			if strings.Contains(f[1], " ") {
				fl, _ = u.git(time.Minute, "diff", "--name-only", f[0]+"^1", f[0], "--", mod+"/")
			} else {
				fl, _ = u.git(time.Minute, "show", "--name-only", "--format=", f[0], "--", mod+"/")
			}
			res = append(res, u.mkItem(mod, "cut", f[0], f[2], strings.Split(fl, "\n")))
		}
	}
	return res, nil
}

var updTaskRe = regexp.MustCompile(`(?:^|[^\d.])0?([27]\d{5})(?:[^\d.]|$)`)

func taskIDs(s string) []string {
	var ids []string
	for _, m := range updTaskRe.FindAllStringSubmatch(s, -1) {
		ids = append(ids, m[1])
	}
	return ids
}

type updStats struct {
	commits, authors, mrs, direct, cut, testOnlyMR int
	topAuthors                                     []string
	files, code, lang                              int
}

func (u *updater) stats(mod, prev, cur, mFrom string, its []updItem) updStats {
	var s updStats
	if out, err := u.git(3*time.Minute, "log", "--no-merges", "--cherry-pick", "--right-only", "--format=%an", prev+"..."+cur, "--", mod+"/"); err == nil {
		cnt := map[string]int{}
		for _, a := range strings.Split(strings.TrimSpace(out), "\n") {
			if a != "" {
				cnt[a]++
				s.commits++
			}
		}
		s.authors = len(cnt)
		type kv struct {
			k string
			v int
		}
		var kvs []kv
		for k, v := range cnt {
			kvs = append(kvs, kv{k, v})
		}
		sort.Slice(kvs, func(i, j int) bool { return kvs[i].v > kvs[j].v || kvs[i].v == kvs[j].v && kvs[i].k < kvs[j].k })
		for i, x := range kvs {
			if i == 5 {
				break
			}
			s.topAuthors = append(s.topAuthors, fmt.Sprintf("%s %d", x.k, x.v))
		}
	}
	if out, err := u.git(2*time.Minute, "diff", "--name-only", mFrom, cur, "--", mod+"/"); err == nil {
		for _, f := range strings.Split(strings.TrimSpace(out), "\n") {
			if f == "" {
				continue
			}
			s.files++
			switch a := updArea(mod, f); {
			case a == "#переводы":
				s.lang++
			case !strings.HasPrefix(a, "#"):
				s.code++
			}
		}
	}
	for _, it := range its {
		switch it.kind {
		case "mr":
			s.mrs++
			if it.testOnly {
				s.testOnlyMR++
			}
		case "direct":
			s.direct++
		case "cut":
			s.cut++
		}
	}
	return s
}

func fmtN(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	return s[:len(s)-3] + " " + s[len(s)-3:]
}

// deltaContext: задачи #team (доска помощника) и задачи, где есть твои коммиты.
func (u *updater) deltaContext(mod, prev, cur string) string {
	var b strings.Builder
	if d, err := config.GetConfigDir(); err == nil {
		bin := filepath.Join(d, "assistant", "bin", "delta-board")
		if _, err := os.Stat(bin); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			out, err := exec.CommandContext(ctx, "python3", bin, "--json").Output()
			cancel()
			var r struct {
				Tasks []struct {
					ID    any    `json:"id"`
					Title string `json:"title"`
				} `json:"tasks"`
			}
			if err == nil && json.Unmarshal(out, &r) == nil && len(r.Tasks) > 0 {
				b.WriteString("Открытые задачи #team на доске команды:\n")
				for _, t := range r.Tasks {
					fmt.Fprintf(&b, "- %v %s\n", t.ID, clipRunes(t.Title, 100))
				}
			} else {
				b.WriteString("(доску #team прочитать не удалось)\n")
			}
		}
	}
	me, _ := u.git(10*time.Second, "config", "user.name")
	if me = strings.TrimSpace(me); me != "" {
		out, err := u.git(2*time.Minute, "log", "--no-merges", "--cherry-pick", "--right-only", "--author="+me, "--format=%s", prev+"..."+cur, "--", mod+"/")
		if err == nil && strings.TrimSpace(out) != "" {
			cnt := map[string]int{}
			var order []string
			for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
				for _, id := range taskIDs(l) {
					if cnt[id] == 0 {
						order = append(order, id)
					}
					cnt[id]++
				}
			}
			if len(order) > 0 {
				fmt.Fprintf(&b, "Задачи, где есть коммиты пользователя (%s, QA): ", me)
				for i, id := range order {
					if i > 0 {
						b.WriteString(", ")
					}
					fmt.Fprintf(&b, "%s (%d)", id, cnt[id])
				}
				b.WriteString("\nЕго коммиты по задачам (для статуса QA):\n")
				n := 0
				for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
					if len(taskIDs(l)) > 0 && n < 40 {
						b.WriteString("- " + clipRunes(l, 140) + "\n")
						n++
					}
				}
			}
		}
	}
	return b.String()
}

var (
	updMergeTail = regexp.MustCompile(`\s*\(merge branch '[^']*'( into '[^']*')?\)`)
	updBarista   = regexp.MustCompile(`\s*\[(barista[^\]]*|original cherry-pick[^\]]*|ci skip)\]`)
)

func cleanSubj(s string) string {
	return strings.TrimSpace(updBarista.ReplaceAllString(updMergeTail.ReplaceAllString(s, ""), ""))
}

// itemLines: строки для модели. То, что затрагивает только тесты, доки, переводы и сборку,
// не перечисляется по одному — только итог: так запрос короче в разы.
func itemLines(its []updItem) string {
	var b strings.Builder
	quiet := map[string]int{}
	var quietIDs []string
	seenID := map[string]bool{}
	for _, it := range its {
		if it.testOnly {
			kind := "тесты"
			for _, a := range it.areas {
				if a == "#переводы" {
					kind = "переводы"
				}
			}
			if strings.Contains(strings.ToLower(it.subj), "барист") || strings.Contains(it.subj, "barista") || strings.Contains(it.subj, "Knowledge") {
				kind = "служебное"
			}
			quiet[kind]++
			for _, id := range taskIDs(it.subj) {
				if !seenID[id] {
					seenID[id] = true
					quietIDs = append(quietIDs, id)
				}
			}
			continue
		}
		kind := map[string]string{"mr": "MR", "direct": "commit", "cut": "ПОСЛЕ_СРЕЗА"}[it.kind]
		var code []string
		for _, a := range it.areas {
			if !strings.HasPrefix(a, "#") {
				code = append(code, a)
			}
		}
		fmt.Fprintf(&b, "%s|%s|%d|%s|%s\n", kind, it.hash[:8], it.files, strings.Join(code, ","), clipRunes(cleanSubj(it.subj), 170))
	}
	if len(quiet) > 0 {
		fmt.Fprintf(&b, "Без кода продукта (только тесты, доки, переводы, сборка): тесты %d, переводы %d, служебное %d", quiet["тесты"], quiet["переводы"], quiet["служебное"])
		if len(quietIDs) > 0 {
			if len(quietIDs) > 40 {
				quietIDs = quietIDs[:40]
			}
			b.WriteString("; задачи в них: " + strings.Join(quietIDs, ", "))
		}
		b.WriteString("\n")
	}
	return b.String()
}

const updPromptRules = `Пиши по-русски, коротко, без воды. Ссылки на задачи пиши строго так: «Б24 782059» для номеров на 7 и «M 261560» для номеров на 2 (ведущий 0 убирай) — ссылки подставятся сами. Если номера нет — короткий хеш из входных данных (8 символов). Изменения из строк ПОСЛЕ_СРЕЗА помечай «после среза». Не выдумывай того, чего нет во входных данных; где не уверен, что видит пользователь, пиши осторожно.`

func fullPrompt(mod, ver string, its []updItem, delta string) string {
	return fmt.Sprintf(`Ты помогаешь QA команды чатов разобрать обновление модуля %[1]s %[2]s, которое ушло в тестирование. Ниже список того, что в него вошло: тип|хеш|файлов|затронутые зоны кода|заголовок. Зоны: js:… — фронт, php:… — бэк, апдейтер — код обновления портала, #тесты/#docs/#переводы/#dist — не код.

%[3]s

Контекст #team:
%[4]s

Выведи ровно четыре раздела в Markdown, без вступления и заключения:

## Касается #team
Таблица «| Задача | Что поменялось | QA |» — только задачи из контекста #team, которые есть во входных данных. В колонке QA — что видно по коммитам пользователя (бриф, C-01…, итог, что отложено), иначе «—». Если таких задач нет — одна строка «Задач #team в обновлении не нашёл».

## Что поменялось
Группы «### <фича или зона> » (5–8 групп по смыслу для пользователя: папки, ChatBot, поле ввода, список чатов, пересылка и поиск, прочее…). В каждой — пункты «- что поменялось для пользователя одной строкой — ссылка». Только изменения, которые пользователь может заметить. Самая крупная или рискованная группа — первой, в скобках после названия коротко почему.

## Без видимых изменений
4–6 пунктов «- **Тема:** кратко» — рефакторинг, фича-флаги, фиксы падений и прав, переводы, тесты (сколько MR), сборка и зависимости.

## Топ-5 зон риска
Пять зон, где изменения вероятнее всего что-то сломали (крупные изменения, апдейтеры, много фиксов подряд, правки после среза, общие компоненты). Для каждой:
**N. Название**
Почему: одна строка.
- сценарий: действие → ожидаемый результат (2–3 сценария на зону, конкретные, проверяемые руками на портале)

%[5]s`, mod, ver, itemLines(its), delta, updPromptRules)
}

func addPrompt(mod, ver string, prev string, its []updItem, delta string) string {
	return fmt.Sprintf(`Обновление %[1]s %[2]s уже разобрано (дайджест ниже). С тех пор в ветку обновления долили новые изменения — разбери только их.

Новые изменения (тип|хеш|файлов|зоны|заголовок):
%[3]s
Контекст #team:
%[4]s
Прошлый дайджест:
<<<
%[5]s
>>>

Выведи только раздел, без вступления:

## Дополнение %[6]s — новых изменений: %[7]d
- что поменялось для пользователя одной строкой — ссылка (пометь «#team», если задача из контекста)
Затем, если новые изменения задевают зоны риска из прошлого дайджеста или создают новые, — подзаголовок «### Что перепроверить» и 1–4 сценария «действие → ожидание». Если нечего — не пиши подзаголовок.

%[8]s`, mod, ver, itemLines(its), delta, clipRunes(prev, 9000), time.Now().Format("02.01 15:04"), len(its), updPromptRules)
}

var updRunModel = func(u *updater, prompt string) (string, error) {
	tmp, err := os.MkdirTemp("", "squad-digest-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	try := func(args ...string) (string, string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), updModelLimit)
		defer cancel()
		c := exec.CommandContext(ctx, u.program, args...)
		c.Dir = tmp
		c.Stdin = strings.NewReader(prompt)
		var out, errb bytes.Buffer
		c.Stdout, c.Stderr = &out, &errb
		err := c.Run()
		return out.String(), errb.String(), err
	}
	// без пользовательских хуков (уведомления, сторож) и без инструментов
	out, stderr, err := try("-p", "--model", u.model, "--output-format", "text", "--setting-sources", "project", "--tools", "", "--strict-mcp-config")
	if err != nil && strings.Contains(stderr+out, "unknown option") {
		out, stderr, err = try("-p", "--model", u.model, "--output-format", "text")
	}
	if err != nil {
		return "", fmt.Errorf("модель: %v %s", err, clipRunes(strings.TrimSpace(stderr), 200))
	}
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("модель ответила пусто")
	}
	return strings.TrimSpace(out), nil
}

var (
	updLinkB24 = regexp.MustCompile(`(^|[^\[\w])Б24 0?(7\d{5})\b`)
	updLinkM   = regexp.MustCompile(`(^|[^\[\w])M 0?(2\d{5})\b`)
)

func linkTasks(s string) string {
	s = updLinkB24.ReplaceAllString(s, "$1[Б24 $2](https://tracker.example.com/task/$2/)")
	return updLinkM.ReplaceAllString(s, "$1[M $2](https://bugs.example.com/view.php?id=$2)")
}

func (u *updater) digestPath(k string) string {
	return filepath.Join(u.dir, strings.ReplaceAll(k, "/", "-")+".md")
}

func (u *updater) setStatus(k, st, msg string) {
	u.mu.Lock()
	if e := u.E[k]; e != nil {
		e.Status, e.Err = st, msg
	}
	u.save()
	u.mu.Unlock()
}

// digest: полный разбор или дополнение (если разбор уже есть и в ветке новые коммиты).
func (u *updater) digest(k string) {
	u.mu.Lock()
	e := u.E[k]
	if e == nil {
		u.mu.Unlock()
		return
	}
	ee := *e
	e.Status, e.Err = "running", ""
	u.save()
	u.mu.Unlock()
	fail := func(err error) {
		log.WarningLog.Printf("updates %s: %v", k, err)
		u.setStatus(k, "error", err.Error())
	}
	mod := ee.Module
	rs, err := u.refs(mod)
	if err != nil {
		fail(err)
		return
	}
	var prev, cur string
	for _, r := range rs {
		if r.ver == ee.Version {
			cur = branchOf(mod, r.ver, r.closed)
			break
		}
		prev = branchOf(mod, r.ver, r.closed)
	}
	if cur == "" {
		fail(fmt.Errorf("нет ветки update/%s/%s — нажми «Обновить список»", mod, ee.Version))
		return
	}
	if prev == "" {
		fail(fmt.Errorf("нет предыдущей ветки обновления %s — не с чем сравнить", mod))
		return
	}
	head, err := u.git(20*time.Second, "rev-parse", cur)
	if err != nil {
		fail(err)
		return
	}
	head = strings.TrimSpace(head)
	mbCur, err1 := u.git(time.Minute, "merge-base", cur, "origin/master")
	mbPrev, err2 := u.git(time.Minute, "merge-base", prev, "origin/master")
	if err1 != nil || err2 != nil {
		fail(fmt.Errorf("не нашёл, откуда срезаны ветки: %v %v", err1, err2))
		return
	}
	mbCur, mbPrev = strings.TrimSpace(mbCur), strings.TrimSpace(mbPrev)
	path := u.digestPath(k)
	old, _ := os.ReadFile(path)
	addMode := len(old) > 0 && ee.DoneHead != "" && ee.DoneHead != head
	if addMode {
		if err := exec.Command("git", "-C", u.repo, "merge-base", "--is-ancestor", ee.DoneHead, head).Run(); err != nil {
			addMode = false // ветку перезаписали — разбираем заново
		}
	}
	if len(old) > 0 && ee.DoneHead == head {
		u.setStatus(k, "", "")
		return
	}
	delta := u.deltaContext(mod, prev, cur)
	if addMode {
		its, err := u.items(mod, "", "", ee.DoneHead, head)
		if err != nil {
			fail(err)
			return
		}
		if len(its) == 0 {
			u.finish(k, head, "")
			return
		}
		body, err := updRunModel(u, addPrompt(mod, ee.Version, string(old), its, delta))
		if err != nil {
			fail(err)
			return
		}
		u.finish(k, head, strings.TrimRight(string(old), "\n")+"\n\n"+linkTasks(body)+"\n")
		return
	}
	its, err := u.items(mod, mbPrev, mbCur, mbCur, head)
	if err != nil {
		fail(err)
		return
	}
	st := u.stats(mod, prev, cur, mbPrev, its)
	body, err := updRunModel(u, fullPrompt(mod, ee.Version, its, delta))
	if err != nil {
		fail(err)
		return
	}
	short := func(h string) string { return h[:min(8, len(h))] }
	var b strings.Builder
	fmt.Fprintf(&b, "# Обновление %s %s — разбор\n\n", mod, ee.Version)
	fmt.Fprintf(&b, "**Ветка:** `update/%s/%s`, срезана с master (%s)  \n", mod, ee.Version, short(mbCur))
	fmt.Fprintf(&b, "**Сравнение с:** `%s` (срез %s)  \n", strings.TrimPrefix(prev, "origin/"), short(mbPrev))
	fmt.Fprintf(&b, "**Разобрано:** %s по коммит %s. «Дополнить» разберёт только то, что новее.\n\n", time.Now().Format("02.01 15:04"), short(head))
	b.WriteString("| Объём | |\n|---|---|\n")
	fmt.Fprintf(&b, "| Коммиты | %s (без мержей и повторов от черри-пиков) |\n", fmtN(st.commits))
	fmt.Fprintf(&b, "| Как пришли | %d MR в master · %d прямых коммитов · %d в ветку после среза |\n", st.mrs, st.direct, st.cut)
	fmt.Fprintf(&b, "| Файлы | %s всего · %s кода (без dist, тестов, доков и переводов) · %d переводов |\n", fmtN(st.files), fmtN(st.code), st.lang)
	fmt.Fprintf(&b, "| Авторы | %d; больше всех: %s |\n", st.authors, strings.Join(st.topAuthors, ", "))
	fmt.Fprintf(&b, "| Только тесты | %d MR из %d — на пользователя не влияют |\n\n", st.testOnlyMR, st.mrs)
	b.WriteString(linkTasks(body) + "\n")
	u.finish(k, head, b.String())
}

func (u *updater) finish(k, head, text string) {
	if text != "" {
		if err := os.WriteFile(u.digestPath(k), []byte(text), 0o644); err != nil {
			u.setStatus(k, "error", err.Error())
			return
		}
	}
	u.mu.Lock()
	if e := u.E[k]; e != nil {
		e.Status, e.Err, e.DoneHead, e.DoneAt, e.New = "", "", head, time.Now().Format("02.01 15:04"), 0
	}
	u.save()
	u.mu.Unlock()
	notifyUser("orchestraByVitalik: разбор", "Разбор обновления "+strings.ReplaceAll(k, "/", " ")+" готов")
}

// ---------- веб ----------

type updView struct {
	updEntry
	Key    string `json:"key"`
	Digest bool   `json:"digest"`
}

func (u *updater) list() map[string]any {
	u.mu.Lock()
	defer u.mu.Unlock()
	var vs []updView
	for k, e := range u.E {
		if (e.Closed || e.Gone) && e.DoneHead == "" && !e.Manual {
			continue
		}
		_, err := os.Stat(u.digestPath(k))
		vs = append(vs, updView{updEntry: *e, Key: k, Digest: err == nil})
	}
	sort.Slice(vs, func(i, j int) bool {
		a, b := vs[i], vs[j]
		if a.InTest != b.InTest {
			return a.InTest
		}
		if a.Module != b.Module {
			return a.Module < b.Module
		}
		return verLess(b.Version, a.Version)
	})
	return map[string]any{"items": vs, "chat_err": u.ChatErr, "scan_at": u.ScanAt, "modules": u.modules, "chat": u.chat}
}

func (u *updater) webList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(u.list())
}

var updMdLink = regexp.MustCompile(`\[([^\]]+)\]\((https?://[^)\s"]+)\)`)

func (u *updater) webView(w http.ResponseWriter, r *http.Request) {
	k := r.URL.Query().Get("k")
	u.mu.Lock()
	_, ok := u.E[k]
	u.mu.Unlock()
	if !ok {
		http.Error(w, "нет такого обновления", http.StatusNotFound)
		return
	}
	b, err := os.ReadFile(u.digestPath(k))
	if err != nil {
		http.Error(w, "разбора ещё нет", http.StatusNotFound)
		return
	}
	h := updMdLink.ReplaceAllStringFunc(mdHTML(string(b)), func(s string) string {
		m := updMdLink.FindStringSubmatch(s)
		return `<a href="` + m[2] + `" target="_blank" rel="noopener">` + m[1] + `</a>`
	})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, h)
}

func (u *updater) webDo(w http.ResponseWriter, r *http.Request) {
	switch r.FormValue("do") {
	case "scan":
		go u.scan(true)
		fmt.Fprint(w, "ok")
	case "add":
		mod := strings.ToLower(strings.TrimSpace(r.FormValue("module")))
		ver := strings.TrimSpace(r.FormValue("version"))
		if !updVerRe.MatchString(ver) || mod == "" || strings.ContainsAny(mod, "/ .") {
			http.Error(w, "нужно: модуль и версия, например im 26.1700.0", http.StatusBadRequest)
			return
		}
		u.mu.Lock()
		k := mod + "/" + ver
		if u.E[k] == nil {
			u.E[k] = &updEntry{Module: mod, Version: ver}
		}
		u.E[k].Manual = true
		known := false
		for _, m := range u.modules {
			known = known || m == mod
		}
		if !known {
			u.modules = append(u.modules, mod)
		}
		u.save()
		u.mu.Unlock()
		go u.scan(true)
		fmt.Fprint(w, "ok")
	case "digest":
		k := r.FormValue("k")
		u.mu.Lock()
		e := u.E[k]
		if e == nil {
			u.mu.Unlock()
			http.Error(w, "нет такого обновления", http.StatusNotFound)
			return
		}
		if e.Status == "queued" || e.Status == "running" {
			u.mu.Unlock()
			http.Error(w, "уже в работе", http.StatusConflict)
			return
		}
		e.Status, e.Err = "queued", ""
		u.save()
		u.mu.Unlock()
		select {
		case u.queue <- k:
		default:
			u.setStatus(k, "error", "очередь переполнена")
		}
		fmt.Fprint(w, "ok")
	case "forget":
		k := r.FormValue("k")
		u.mu.Lock()
		if e := u.E[k]; e != nil {
			e.Manual, e.InTest = false, false
		}
		u.save()
		u.mu.Unlock()
		fmt.Fprint(w, "ok")
	default:
		http.Error(w, "неизвестное действие", http.StatusBadRequest)
	}
}
