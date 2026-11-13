package app

import (
	"bufio"
	"bytes"
	"claude-squad/session"
	"encoding/json"
	"html"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The web view's history comes from the session's Claude Code transcript
// (~/.claude/projects/*/<conversation>.jsonl): the whole conversation, even when the
// terminal's scrollback is short or was cleared by a redraw.

// transcriptPath finds the transcript file of a conversation, "" when there is none.
func transcriptPath(conv string) string {
	if conv == "" || strings.ContainsAny(conv, `/\.`) {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	m, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", conv+".jsonl"))
	if len(m) == 0 {
		return ""
	}
	best, bestT := m[0], int64(0)
	for _, p := range m {
		if st, err := os.Stat(p); err == nil && st.ModTime().UnixNano() > bestT {
			best, bestT = p, st.ModTime().UnixNano()
		}
	}
	return best
}

// firstAsk is the first message the session got — usually the task it was opened for —
// cut to a line or two. Cached: it never changes, and the page asks every couple of seconds.
var (
	askMu    sync.Mutex
	askCache = map[string]askEntry{}
)

type askEntry struct {
	text string
	at   time.Time
}

func firstAsk(conv string) string {
	if conv == "" {
		return ""
	}
	askMu.Lock()
	defer askMu.Unlock()
	if e, ok := askCache[conv]; ok && (e.text != "" || time.Since(e.at) < time.Minute) {
		return e.text
	}
	t := ""
	if p := transcriptPath(conv); p != "" {
		t = readFirstAsk(p)
	}
	askCache[conv] = askEntry{t, time.Now()}
	return t
}

func readFirstAsk(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 32<<20)
	for n := 0; sc.Scan() && n < 400; n++ {
		var e struct {
			Type    string `json:"type"`
			IsMeta  bool   `json:"isMeta"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.IsMeta || e.Type != "user" {
			continue
		}
		text := ""
		var s string
		if json.Unmarshal(e.Message.Content, &s) == nil {
			text = s
		} else {
			var items []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if json.Unmarshal(e.Message.Content, &items) == nil {
				for _, it := range items {
					if it.Type == "text" {
						text = it.Text
						break
					}
				}
			}
		}
		text = strings.TrimSpace(text)
		if text == "" || strings.HasPrefix(text, "<") {
			continue
		}
		return clipAsk(text)
	}
	return ""
}

// clipAsk drops the service note added to workers and squeezes the text into ~220 characters.
func clipAsk(t string) string {
	t = strings.ReplaceAll(t, session.WorkerNoMenuNote, "")
	t = strings.Join(strings.Fields(t), " ")
	if r := []rune(t); len(r) > 220 {
		t = strings.TrimSpace(string(r[:220])) + "…"
	}
	return t
}

// tBlock is one piece of the conversation as the history shows it: the user's message
// ("you"), the agent's answer ("ai"), a tool call ("tool") or the start of its result ("res").
type tBlock struct {
	kind, text, at string // at: "15:04" when it was written ("" for tool results)
}

// lines is how tall the block is, for paging the history by lines.
func (b tBlock) lines() int { return strings.Count(b.text, "\n") + 1 }

// transcriptBlocks reads a transcript into blocks; tool results are cut to 3 lines.
func transcriptBlocks(path string) []tBlock {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	return parseTranscript(f, nil)
}

// parseTranscript appends the blocks of transcript lines read from r to out.
func parseTranscript(r io.Reader, out []tBlock) []tBlock {
	add := func(kind, text, at string, maxLines int) {
		text = strings.TrimRight(strings.ReplaceAll(text, "\r", ""), "\n ")
		if strings.TrimSpace(text) == "" {
			return
		}
		if maxLines > 0 {
			if ls := strings.Split(text, "\n"); len(ls) > maxLines {
				text = strings.Join(ls[:maxLines], "\n") + "\n…"
			}
		}
		out = append(out, tBlock{kind, text, at})
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var e struct {
			Type      string `json:"type"`
			IsMeta    bool   `json:"isMeta"`
			Timestamp string `json:"timestamp"`
			Message   struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.IsMeta || (e.Type != "user" && e.Type != "assistant") {
			continue
		}
		at := ""
		if t, err := time.Parse(time.RFC3339Nano, e.Timestamp); err == nil {
			at = t.Local().Format("15:04")
		}
		var s string
		if json.Unmarshal(e.Message.Content, &s) == nil {
			if e.Type == "user" && !strings.HasPrefix(strings.TrimSpace(s), "<") {
				add("you", s, at, 0)
			}
			continue
		}
		var items []struct {
			Type    string          `json:"type"`
			Text    string          `json:"text"`
			Name    string          `json:"name"`
			Input   json.RawMessage `json:"input"`
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(e.Message.Content, &items) != nil {
			continue
		}
		for _, it := range items {
			switch it.Type {
			case "text":
				if e.Type == "user" {
					if !strings.HasPrefix(strings.TrimSpace(it.Text), "<") {
						add("you", it.Text, at, 0)
					}
				} else {
					add("ai", it.Text, at, 0)
				}
			case "tool_use":
				add("tool", it.Name+"("+toolSummary(it.Input)+")", at, 0)
			case "tool_result":
				add("res", resultText(it.Content), "", 3)
			}
		}
	}
	return out
}

// lastBlocks takes whole blocks from the end until about `lines` lines: a long answer is
// never cut in the middle (it is shown whole or comes with the next page).
func lastBlocks(bs []tBlock, lines int) (page []tBlock, counted int, more bool) {
	k := len(bs)
	for k > 0 && counted < lines {
		k--
		counted += bs[k].lines()
	}
	return bs[k:], counted, k > 0
}

// toolSummary is the gist of a tool call's input: the command, file or pattern.
func toolSummary(raw json.RawMessage) string {
	var in map[string]any
	if json.Unmarshal(raw, &in) != nil {
		return ""
	}
	for _, k := range []string{"command", "file_path", "pattern", "url", "query", "description", "prompt", "path"} {
		if v, ok := in[k].(string); ok && v != "" {
			v = strings.ReplaceAll(v, "\n", " ")
			if r := []rune(v); len(r) > 140 {
				v = string(r[:140]) + "…"
			}
			return v
		}
	}
	return ""
}

func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var items []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &items) == nil {
		var b []string
		for _, it := range items {
			if it.Type == "text" {
				b = append(b, it.Text)
			}
		}
		return strings.Join(b, "\n")
	}
	return ""
}

// transcriptHTML draws the blocks: your messages on a background with their time, the
// agent's answers as readable text (light markdown: headings, bold, lists, code), tool
// calls small and dim — not a wall of same-looking lines.
func transcriptHTML(bs []tBlock) string {
	var b strings.Builder
	for _, x := range bs {
		switch x.kind {
		case "you":
			b.WriteString(`<div class="t-you">`)
			if x.at != "" {
				b.WriteString(`<span class="t-at">` + x.at + `</span>`)
			}
			b.WriteString(html.EscapeString(x.text) + `</div>`)
		case "ai":
			b.WriteString(`<div class="t-ai">` + mdHTML(x.text) + `</div>`)
		case "tool":
			b.WriteString(`<div class="t-tool">⏺ ` + html.EscapeString(x.text) + `</div>`)
		case "res":
			b.WriteString(`<div class="t-res">⎿ ` + html.EscapeString(x.text) + `</div>`)
		}
	}
	return b.String()
}

var (
	mdBold = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
	mdCode = regexp.MustCompile("`([^`\n]+)`")
	mdHead = regexp.MustCompile(`^#{1,6}\s+`)
	mdList = regexp.MustCompile(`^(\s*)([-*•]|\d+[.)])\s+`)
	// mdTableSep: the |---|:--:| line under a table's header.
	mdTableSep = regexp.MustCompile(`^\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?$`)
	// mdNeed: a line where the agent asks the user to decide or act.
	mdBr = regexp.MustCompile(`(?i)&lt;br\s*/?&gt;`)
	mdNeed = regexp.MustCompile(`(?i)нужен ты|нужно твоё решение|нужно твое решение|⚠`)
)

func isTableRow(l string) bool {
	t := strings.TrimSpace(l)
	return strings.HasPrefix(t, "|") && strings.Count(t, "|") >= 2
}

func tableCells(l string) []string {
	t := strings.TrimSpace(l)
	t = strings.TrimSuffix(strings.TrimPrefix(t, "|"), "|")
	cells := strings.Split(t, "|")
	for i, c := range cells {
		e := html.EscapeString(strings.TrimSpace(c))
		e = mdBr.ReplaceAllString(e, "<br>") // agents break lines inside a cell with <br>
		e = mdCode.ReplaceAllString(e, "<code>$1</code>")
		cells[i] = mdBold.ReplaceAllString(e, "<b>$1</b>")
	}
	return cells
}

// mdTable draws a markdown table; a wide one scrolls in its box.
func mdTable(head string, rows []string) string {
	var b strings.Builder
	b.WriteString(`<div class="t-tblw"><table class="t-tbl"><tr>`)
	for _, c := range tableCells(head) {
		b.WriteString("<th>" + c + "</th>")
	}
	b.WriteString("</tr>")
	for _, r := range rows {
		b.WriteString("<tr>")
		for _, c := range tableCells(r) {
			b.WriteString("<td>" + c + "</td>")
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</table></div>")
	return b.String()
}

// mdHTML renders the little markdown Claude writes: code fences, headings, lists,
// **bold** and `code`. Everything is escaped first; only these tags are added.
func mdHTML(text string) string {
	var b strings.Builder
	inCode := false
	lines := strings.Split(text, "\n")
	for k := 0; k < len(lines); k++ {
		l := lines[k]
		if !inCode && isTableRow(l) && k+1 < len(lines) && mdTableSep.MatchString(strings.TrimSpace(lines[k+1])) {
			end := k + 2
			for end < len(lines) && isTableRow(lines[end]) {
				end++
			}
			b.WriteString(mdTable(lines[k], lines[k+2:end]))
			k = end - 1
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			if inCode {
				b.WriteString("</div>")
			} else {
				b.WriteString(`<div class="t-code">`)
			}
			inCode = !inCode
			continue
		}
		e := html.EscapeString(l)
		if inCode {
			b.WriteString(e + "\n")
			continue
		}
		e = mdCode.ReplaceAllString(e, "<code>$1</code>")
		e = mdBold.ReplaceAllString(e, "<b>$1</b>")
		switch {
		case mdHead.MatchString(e):
			b.WriteString(`<div class="t-h">` + mdHead.ReplaceAllString(e, "") + `</div>`)
		case mdList.MatchString(e):
			m := mdList.FindStringSubmatch(e)
			bu := m[2]
			if bu == "-" || bu == "*" || bu == "•" {
				bu = "•"
			}
			// flush with the text around it (nested lists step in); wrapped lines hang under the text, not the bullet
			b.WriteString(`<div class="t-li" style="padding-left:` + strconv.Itoa(len(m[1])/2) + `.0em"><span class="t-bu">` + bu + `</span><span class="t-lt">` + e[len(m[0]):] + `</span></div>`)
		case strings.TrimSpace(e) == "":
			b.WriteString(`<div class="t-gap"></div>`)
		case mdNeed.MatchString(l):
			b.WriteString(`<div class="t-need">` + e + `</div>`)
		default:
			b.WriteString(`<div>` + e + `</div>`)
		}
	}
	if inCode {
		b.WriteString("</div>")
	}
	return b.String()
}

// blockCache keeps each transcript's blocks and how far the file was read: a transcript
// only grows, so the chat view (polled every 2 s while open) parses just the new lines.
var blockCache = struct {
	sync.Mutex
	m map[string]*cachedBlocks
}{m: map[string]*cachedBlocks{}}

type cachedBlocks struct {
	off    int64
	blocks []tBlock
}

func transcriptBlocksCached(path string) []tBlock {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil
	}
	blockCache.Lock()
	defer blockCache.Unlock()
	c := blockCache.m[path]
	if c == nil || st.Size() < c.off { // new, or rewritten shorter
		c = &cachedBlocks{}
		blockCache.m[path] = c
	}
	if st.Size() > c.off {
		buf := make([]byte, st.Size()-c.off)
		n, _ := f.ReadAt(buf, c.off)
		buf = buf[:n]
		end := bytes.LastIndexByte(buf, '\n') + 1 // a line still being written waits
		if end > 0 {
			c.blocks = parseTranscript(bytes.NewReader(buf[:end]), c.blocks)
			c.off += int64(end)
		}
	}
	return c.blocks[:len(c.blocks):len(c.blocks)]
}
