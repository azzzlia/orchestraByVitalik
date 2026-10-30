package ui

import (
	"claude-squad/session"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var queueKindColor = map[session.QueueKind]string{
	session.QueueCalls:      "#ef4444",
	session.QueuePermission: "#f59e0b",
	session.QueueTrust:      "#f59e0b",
	session.QueueMenu:       "#a78bfa",
	session.QueueDecision:   "#38bdf8",
	session.QueueWaiting:    "#9ca3af",
}

// QueueKeysHint lists what the keys do for the selected row.
func QueueKeysHint(it *session.QueueItem) string {
	h := "↑↓ · enter открыть · i написать ему · d дирижёру"
	if it != nil {
		switch {
		case it.Answerable():
			h += " · 1–9 вариант"
		case it.Kind == session.QueuePermission:
			h += " · y разрешить раз"
		}
	}
	return h + " · u назад"
}

func waited(since time.Time, now time.Time) string {
	if since.IsZero() {
		return ""
	}
	d := now.Sub(since)
	switch {
	case d < time.Minute:
		return "только что"
	case d < time.Hour:
		return fmt.Sprintf("%d мин", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d ч %d мин", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%d д", int(d.Hours()/24))
}

// RenderQueue draws the «ждут тебя» screen: one block per session, the selected one
// with its menu options (or the full command of a permission request).
func RenderQueue(items []session.QueueItem, selected, width, height int, now time.Time) string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#a78bfa")).
		Render(fmt.Sprintf(" Ждут тебя (%d)", len(items)))
	var sel *session.QueueItem
	if selected >= 0 && selected < len(items) {
		sel = &items[selected]
	}
	header := title + overviewDim.Render("   "+QueueKeysHint(sel))
	if lipgloss.Width(header) > width {
		header = ansi.Truncate(header, width-1, "…")
	}
	if len(items) == 0 {
		return header + "\n\n  Никто не ждёт — всё идёт само. 🎉"
	}
	inner := width - 4
	if inner < 20 {
		inner = 20
	}
	fit := func(s string) string {
		if lipgloss.Width(s) > inner {
			return ansi.Truncate(s, inner-1, "…")
		}
		return s
	}
	var blocks [][]string
	for i, it := range items {
		num := it.Inst.Number
		if num == 0 {
			num = it.Index + 1
		}
		tag := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(queueKindColor[it.Kind])).
			Render(fmt.Sprintf("%-10s", it.KindLabel()))
		head := fmt.Sprintf("%s #%d %s", tag, num, it.Inst.Title)
		if w := waited(it.Since, now); w != "" {
			head += overviewDim.Render("  · ждёт " + w)
		}
		lines := []string{fit(head)}
		text := strings.Join(strings.Fields(it.Text), " ")
		if i == selected && it.Kind == session.QueueMenu && it.Inst.Menu != nil && it.Inst.Menu.Question != "" {
			text = it.Inst.Menu.Question // the options follow one per line
		}
		if i == selected {
			// The selected row in full: the whole command, every option.
			for _, l := range wrapText(text, inner-2) {
				lines = append(lines, fit("  "+l))
			}
			if it.Kind == session.QueueMenu && it.Inst.Menu != nil {
				for _, o := range it.Inst.Menu.Options {
					l := fmt.Sprintf("    %d. %s", o.Number, o.Label)
					if o.Desc != "" {
						l += overviewDim.Render(" — " + o.Desc)
					}
					lines = append(lines, fit(l))
				}
			}
		} else {
			lines = append(lines, fit("  "+overviewDim.Render(text)))
		}
		blocks = append(blocks, lines)
	}
	// Keep the selected block on screen: skip blocks from the top until it fits.
	// Budget: the window minus the header and the blank line under it.
	budget := height - 2
	size := func(i int) int { return len(blocks[i]) + 1 } // block + blank line
	if budget < 5 {
		return header
	}
	// «… выше», the block, its blank line and «… ниже» must fit: cut a huge block.
	if sel != nil && len(blocks[selected])+3 > budget {
		blocks[selected] = blocks[selected][:budget-3]
	}
	first := 0
	for first < selected {
		n := 1 // «… выше»
		for i := first; i <= selected; i++ {
			n += size(i)
		}
		if n+1 <= budget { // +1 for a possible «… ниже»
			break
		}
		first++
	}
	var out []string
	used := 0
	if first > 0 {
		out = append(out, overviewDim.Render(fmt.Sprintf("  … выше ещё %d", first)))
		used++
	}
	for i := first; i < len(blocks); i++ {
		need := size(i)
		if i < len(blocks)-1 {
			need++ // room for «… ниже» if the next one does not fit
		}
		if used+need > budget && i != selected {
			out = append(out, overviewDim.Render(fmt.Sprintf("  … ниже ещё %d", len(blocks)-i)))
			break
		}
		box := lipgloss.NewStyle().PaddingLeft(1)
		if i == selected {
			box = box.Border(lipgloss.ThickBorder(), false, false, false, true).
				BorderForeground(lipgloss.Color("#a78bfa")).PaddingLeft(0)
		}
		out = append(out, box.Render(strings.Join(blocks[i], "\n")), "")
		used += size(i)
	}
	return header + "\n\n" + strings.TrimRight(strings.Join(out, "\n"), "\n")
}

// wrapText breaks text into lines of at most w cells.
func wrapText(s string, w int) []string {
	if w < 10 {
		w = 10
	}
	var lines []string
	cur := ""
	for _, word := range strings.Fields(s) {
		if cur != "" && lipgloss.Width(cur)+1+lipgloss.Width(word) > w {
			lines = append(lines, cur)
			cur = ""
		}
		if cur != "" {
			cur += " "
		}
		cur += word
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	if len(lines) > 6 {
		lines = append(lines[:6], "…")
	}
	return lines
}
