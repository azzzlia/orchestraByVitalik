package app

import (
	"fmt"
	"html"
	"net/http"
	"os"
	"strings"

	"claude-squad/session"
)

// The session as a chat (the web view's «Чат» mode): the conversation from Claude Code's
// transcript laid out as a feed — time, who, the message. The agent's tool calls become
// small chips over its answer; their output stays in the terminal view.

// chatHTML lays out blocks as the feed: one row per message; a Claude row holds the
// agent's turn in order — chips for its tool calls, then its text, and so on.
func chatHTML(bs []tBlock) string {
	var b strings.Builder
	var items []string     // the Claude row's parts, in order
	var chips []string     // the current run of tool calls
	var toolMedia []string // pictures the tool calls of the current run named
	at := ""
	lastTool, lastN := "", 0
	closeChips := func() {
		if len(chips) > 0 {
			// a long run of commands folds: the first few, the rest behind «ещё N»
			const show = 4
			if len(chips) > show+2 {
				items = append(items, `<div class="c-chips">`+strings.Join(chips[:show], "")+
					`<details class="c-xchips"><summary>ещё `+fmt.Sprint(len(chips)-show)+`</summary><div class="c-chips">`+strings.Join(chips[show:], "")+`</div></details></div>`)
			} else {
				items = append(items, `<div class="c-chips">`+strings.Join(chips, "")+`</div>`)
			}
			chips, lastTool = nil, ""
		}
		if len(toolMedia) > 0 {
			items = append(items, mediaHTML(toolMedia))
			toolMedia = nil
		}
	}
	flush := func() {
		closeChips()
		if len(items) > 0 {
			b.WriteString(`<div class="c-row"><span class="c-at">` + at + `</span><div class="c-body"><span class="c-who c-ai">Claude</span>` + strings.Join(items, "") + `</div></div>`)
		}
		items, at = nil, ""
	}
	addChip := func(text string) {
		name, arg := text, ""
		if i := strings.IndexByte(text, '('); i > 0 {
			name, arg = text[:i], strings.TrimSuffix(text[i+1:], ")")
		}
		if name == lastTool && (name == "Read" || name == "Grep" || name == "Glob" || arg == "") {
			lastN++
			chips[len(chips)-1] = chip(name, fmt.Sprintf("×%d", lastN), "")
			return
		}
		lastTool, lastN = name, 1
		short := arg
		if r := []rune(short); len(r) > 60 {
			short = string(r[:60]) + "…"
		}
		chips = append(chips, chip(name, short, arg))
	}
	for _, x := range bs {
		switch x.kind {
		case "you":
			flush()
			who, cls := "Запрос", "c-you"
			text := strings.TrimSpace(strings.ReplaceAll(x.text, session.WorkerNoMenuNote, ""))
			if strings.HasPrefix(text, "[squad]") {
				who, cls = "Squad", "c-sq"
				text = strings.TrimSpace(strings.TrimPrefix(text, "[squad]"))
			}
			b.WriteString(`<div class="c-row"><span class="c-at">` + x.at + `</span><div class="c-body"><span class="c-who ` + cls + `">` + who + `</span><div class="c-txt">` + html.EscapeString(text) + `</div>` + mediaHTML(mediaPaths(text)) + `</div></div>`)
		case "tool":
			if at == "" {
				at = x.at
			}
			addChip(x.text)
			for _, p := range mediaPaths(x.text) {
				if len(toolMedia) < 6 && !contains(toolMedia, p) {
					toolMedia = append(toolMedia, p)
				}
			}
		case "ai":
			if at == "" {
				at = x.at
			}
			closeChips()
			items = append(items, `<div class="c-md">`+mdHTML(x.text)+`</div>`+mediaHTML(mediaPaths(x.text)))
		}
	}
	flush()
	return b.String()
}

func chip(name, short, full string) string {
	title := ""
	if full != "" && full != short {
		title = ` title="` + html.EscapeString(full) + `"`
	}
	s := html.EscapeString(name)
	if short != "" {
		s += " " + html.EscapeString(short)
	}
	return `<span class="c-chip"` + title + `>` + s + `</span>`
}

// webChat serves the feed: from — the first block shown (the page keeps it so the feed
// does not jump as it grows), or the last `lines` lines when from < 0; v — the version
// the page has, answered with {same:true} when the transcript did not change.
func (m *home) webChat(w http.ResponseWriter, r *http.Request) {
	n := formInt(r, "n")
	m.mu.Lock()
	inst := m.findByNumber(n)
	conv := ""
	if inst != nil {
		conv = inst.SnapshotEntry().ConvID
	}
	m.mu.Unlock()
	p := transcriptPath(conv)
	if p == "" {
		writeJSON(w, map[string]any{"none": true})
		return
	}
	st, err := os.Stat(p)
	if err != nil {
		writeJSON(w, map[string]any{"none": true})
		return
	}
	from, lines := -1, formInt(r, "lines")
	if f := r.FormValue("from"); f != "" {
		from = formInt(r, "from")
	}
	if lines <= 0 {
		lines = 1500
	}
	version := fmt.Sprintf("%d.%d.%d.%d", st.Size(), st.ModTime().UnixNano(), from, lines)
	if r.FormValue("v") == version {
		writeJSON(w, map[string]any{"same": true})
		return
	}
	bs := transcriptBlocksCached(p)
	if len(bs) == 0 {
		writeJSON(w, map[string]any{"none": true})
		return
	}
	if from < 0 || from > len(bs) {
		page, _, _ := lastBlocks(bs, lines)
		from = len(bs) - len(page)
		// start at a message, not in the middle of the agent's steps
		for from > 0 && bs[from].kind != "you" && len(bs)-from < len(page)+40 {
			from--
		}
		version = fmt.Sprintf("%d.%d.%d.%d", st.Size(), st.ModTime().UnixNano(), from, lines)
	}
	writeJSON(w, map[string]any{"html": chatHTML(bs[from:]), "from": from, "more": from > 0, "version": version})
}
