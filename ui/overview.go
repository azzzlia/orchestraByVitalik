package ui

import (
	"claude-squad/session"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

const overviewMinCellHeight = 9

// overviewMainMinHeight is the smallest height of the conductor and assistant cells.
const overviewMainMinHeight = 12

// OverviewColumns returns how many columns the overview grid uses for a width.
func OverviewColumns(width, count int) int {
	cols := 1
	switch {
	case width >= 200:
		cols = 3
	case width >= 110:
		cols = 2
	}
	if count > 0 && cols > count {
		cols = count
	}
	return cols
}

// fitLine truncates s (which may contain ANSI codes) to width and pads it.
func fitLine(s string, width int) string {
	plain := session.StripANSI(s)
	if runewidth.StringWidth(plain) > width {
		plain = runewidth.Truncate(plain, width, "…")
	}
	if pad := width - runewidth.StringWidth(plain); pad > 0 {
		plain += strings.Repeat(" ", pad)
	}
	return plain
}

// wrap breaks text into lines of at most width cells.
func wrap(text string, width, maxLines int) []string {
	var lines []string
	cur := ""
	for _, word := range strings.Fields(text) {
		candidate := word
		if cur != "" {
			candidate = cur + " " + word
		}
		if runewidth.StringWidth(candidate) > width && cur != "" {
			lines = append(lines, cur)
			cur = word
			if len(lines) == maxLines {
				break
			}
		} else {
			cur = candidate
		}
	}
	if cur != "" && len(lines) < maxLines {
		lines = append(lines, cur)
	}
	if len(lines) == maxLines && maxLines > 0 {
		lines[maxLines-1] = fitLine(lines[maxLines-1], width)
	}
	return lines
}

var (
	overviewDim    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#6b7280", Dark: "#9ca3af"})
	overviewRecap  = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.AdaptiveColor{Light: "#374151", Dark: "#d1d5db"})
	overviewHeader = lipgloss.NewStyle().Bold(true)
)

func renderOverviewCell(inst *session.Instance, idx int, selected bool, width, height int) string {
	if height < 3 {
		height = 3
	}
	inner := width - 4 // border + padding
	if inner < 10 {
		inner = 10
	}
	var lines []string

	marker := "  "
	if hex, ok := sessionColorHex[inst.Color]; ok {
		marker = lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render("●") + " "
	}
	title := fitLine(fmt.Sprintf("%d. %s", idx, inst.Title), inner-2)
	if inst.CallsUserNow() {
		title = fitLine(fmt.Sprintf("%d. %s  ⚠ НУЖЕН ТЫ", idx, inst.Title), inner-2)
		lines = append(lines, marker+overviewHeader.Foreground(lipgloss.Color("#ef4444")).Blink(true).Render(title))
	} else {
		lines = append(lines, marker+overviewHeader.Render(title))
	}

	status, sw := StatusText(inst, lipgloss.NoColor{})
	if sw > inner {
		status = ansi.Truncate(status, inner, "…") // a wrapped status would make the cell taller
	}
	lines = append(lines, status)

	if note := strings.TrimSpace(inst.Note); note != "" {
		lines = append(lines, overviewDim.Render(fitLine(note, inner)))
	}
	if inst.Recap != "" {
		for _, l := range wrap(inst.Recap, inner, 3) {
			lines = append(lines, overviewRecap.Render(fitLine(l, inner)))
		}
	}
	lines = append(lines, overviewDim.Render(strings.Repeat("─", inner)))

	remaining := height - 2 - len(lines)
	if remaining > 0 {
		tail := inst.TailLines
		if inst.Paused() {
			tail = []string{"на паузе — нажми r, чтобы запустить"}
		} else if len(tail) == 0 {
			tail = []string{"…"}
		}
		if len(tail) > remaining {
			tail = tail[len(tail)-remaining:]
		}
		for _, l := range tail {
			lines = append(lines, fitLine(l, inner))
		}
	}
	for len(lines) < height-2 {
		lines = append(lines, "")
	}
	if len(lines) > height-2 {
		lines = lines[:height-2]
	}

	borderColor := lipgloss.TerminalColor(lipgloss.AdaptiveColor{Light: "#d1d5db", Dark: "#4b5563"})
	if hex, ok := sessionColorHex[inst.Color]; ok {
		borderColor = lipgloss.Color(hex)
	}
	border := lipgloss.RoundedBorder()
	if inst.CallsUserNow() {
		border = lipgloss.DoubleBorder()
		borderColor = lipgloss.Color("#ef4444")
	}
	if selected {
		border = lipgloss.ThickBorder()
		borderColor = lipgloss.AdaptiveColor{Light: "#7c3aed", Dark: "#a78bfa"}
	}
	return lipgloss.NewStyle().
		Border(border).
		BorderForeground(borderColor).
		Padding(0, 1).
		Width(width - 2).
		Render(strings.Join(lines, "\n"))
}

// OverviewLayout splits the overview: the conductor and the assistant big on top,
// working sessions in a grid, sleeping/paused ones folded into one line at the bottom.
// The ints are indexes into the instance list.
type OverviewLayout struct {
	Main  []int
	Grid  []int
	Sleep []int
	Cols  int // grid columns
}

// ComputeOverviewLayout builds the layout for this list and screen width.
func ComputeOverviewLayout(instances []*session.Instance, width int) OverviewLayout {
	var l OverviewLayout
	for i, inst := range instances {
		switch {
		case inst.Conductor || inst.Assistant:
			l.Main = append(l.Main, i)
		case inst.Paused():
			l.Sleep = append(l.Sleep, i)
		default:
			l.Grid = append(l.Grid, i)
		}
	}
	l.Cols = OverviewColumns(width, len(l.Grid))
	if l.Cols < 1 {
		l.Cols = 1
	}
	return l
}

// rows lists the layout row by row, top to bottom (for navigation).
func (l OverviewLayout) rows() [][]int {
	var rows [][]int
	if len(l.Main) > 0 {
		rows = append(rows, l.Main)
	}
	for i := 0; i < len(l.Grid); i += l.Cols {
		end := i + l.Cols
		if end > len(l.Grid) {
			end = len(l.Grid)
		}
		rows = append(rows, l.Grid[i:end])
	}
	if len(l.Sleep) > 0 {
		rows = append(rows, l.Sleep)
	}
	return rows
}

// Move returns the instance index after pressing an arrow ("left", "right", "up",
// "down") with cur selected: left/right walk the sessions in order, up/down jump to
// the row above/below, to the cell at about the same horizontal position.
func (l OverviewLayout) Move(cur int, dir string) int {
	rows := l.rows()
	var order []int
	for _, r := range rows {
		order = append(order, r...)
	}
	if len(order) == 0 {
		return cur
	}
	ri, pi := -1, 0
	for i, r := range rows {
		for j, v := range r {
			if v == cur {
				ri, pi = i, j
			}
		}
	}
	if ri < 0 {
		return order[0]
	}
	switch dir {
	case "left", "right":
		pos := 0
		for i, v := range order {
			if v == cur {
				pos = i
			}
		}
		if dir == "left" && pos > 0 {
			pos--
		} else if dir == "right" && pos < len(order)-1 {
			pos++
		}
		return order[pos]
	case "up", "down":
		ti := ri - 1
		if dir == "down" {
			ti = ri + 1
		}
		if ti < 0 || ti >= len(rows) {
			return cur
		}
		from, to := rows[ri], rows[ti]
		var j int
		if l.isGridRow(ri, len(rows)) && l.isGridRow(ti, len(rows)) {
			j = pi // grid rows share columns
		} else {
			// Rows of different widths: keep the horizontal position on screen. A grid
			// row is l.Cols cells wide even when its last row is not full.
			width := func(ri, n int) int {
				if l.isGridRow(ri, len(rows)) {
					return l.Cols
				}
				return n
			}
			x := (float64(pi) + 0.5) / float64(width(ri, len(from)))
			j = int(x * float64(width(ti, len(to))))
		}
		if j >= len(to) {
			j = len(to) - 1
		}
		return to[j]
	}
	return cur
}

// isGridRow: row ri of n is a grid row (not the top row of the conductor and the
// assistant, not the line of sleeping sessions).
func (l OverviewLayout) isGridRow(ri, n int) bool {
	if len(l.Main) > 0 && ri == 0 {
		return false
	}
	if len(l.Sleep) > 0 && ri == n-1 {
		return false
	}
	return true
}

// RenderOverview draws every session: the conductor and the assistant big on top,
// working sessions in a grid with status, recap and the last lines of the screen,
// sleeping ones in one line at the bottom. selected is the highlighted session.
func RenderOverview(instances []*session.Instance, selected, width, height int) string {
	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#a78bfa")).
		Render(fmt.Sprintf(" Все сессии (%d)", len(instances))) +
		overviewDim.Render("   ←↑↓→ выбрать · i написать · enter открыть · r разбудить · p питомец · v/esc назад")
	if len(instances) == 0 {
		return header + "\n\n  Сессий пока нет."
	}
	if height < 8 {
		return header // too small to draw cells
	}
	var calls []string
	for _, inst := range instances {
		if inst.CallsUserNow() {
			calls = append(calls, inst.Title)
		}
	}
	if len(calls) > 0 {
		header += lipgloss.NewStyle().Bold(true).Blink(true).Foreground(lipgloss.Color("#ef4444")).
			Render("   ⚠ зовёт: " + strings.Join(calls, ", "))
		if lipgloss.Width(header) > width {
			header = ansi.Truncate(header, width-1, "…")
		}
	}
	l := ComputeOverviewLayout(instances, width)
	num := func(idx int) int {
		if instances[idx].Number > 0 {
			return instances[idx].Number
		}
		return idx + 1
	}
	avail := height - 1
	var sleepLine string
	if len(l.Sleep) > 0 {
		avail--
		var parts []string
		for _, idx := range l.Sleep {
			label := fmt.Sprintf("%d %s", num(idx), instances[idx].Title)
			if idx == selected {
				label = lipgloss.NewStyle().Reverse(true).Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#7c3aed", Dark: "#a78bfa"}).Render(" " + label + " ")
			} else {
				label = overviewDim.Render(label)
			}
			parts = append(parts, label)
		}
		sleepLine = overviewDim.Render(" спят: ") + strings.Join(parts, overviewDim.Render(" · "))
		if lipgloss.Width(sleepLine) > width {
			sleepLine = ansi.Truncate(sleepLine, width-1, "…")
		}
	}

	gridRows := (len(l.Grid) + l.Cols - 1) / l.Cols
	mainH := 0
	if len(l.Main) > 0 {
		// The conductor and the assistant get what the grid does not need at its
		// minimum height: at least overviewMainMinHeight, at most half the screen.
		mainH = avail - gridRows*overviewMinCellHeight
		if gridRows == 0 {
			mainH = avail
		} else if mainH > avail/2 {
			mainH = avail / 2
		}
		if mainH < overviewMainMinHeight {
			mainH = overviewMainMinHeight
		}
		if mainH > avail {
			mainH = avail
		}
	}
	gridAvail := avail - mainH
	cellH, visibleRows := overviewMinCellHeight, 0
	if gridRows > 0 && gridAvail < 4 {
		// A small window: the conductor and the assistant keep the screen, the grid is
		// one line «ещё сессии ниже» (cells need at least 3 lines plus that line).
		if mainH > avail-1 {
			mainH = avail - 1
		}
		gridAvail = avail - mainH
	} else if gridRows > 0 {
		cellH = gridAvail / gridRows
		visibleRows = gridRows
		if cellH < overviewMinCellHeight {
			cellH = overviewMinCellHeight
			visibleRows = (gridAvail - 1) / cellH // one line for «… ещё сессии ниже»
			if visibleRows < 1 {
				visibleRows = 1
				cellH = gridAvail - 1
				if cellH < 3 {
					cellH = 3
				}
			}
		}
	}

	var rendered []string
	if len(l.Main) > 0 {
		w := width / len(l.Main)
		var cells []string
		for _, idx := range l.Main {
			cells = append(cells, renderOverviewCell(instances[idx], num(idx), idx == selected, w, mainH))
		}
		rendered = append(rendered, lipgloss.JoinHorizontal(lipgloss.Top, cells...))
	}
	if gridRows > 0 {
		firstRow := 0
		for gi, idx := range l.Grid {
			if idx == selected && gi/l.Cols >= visibleRows {
				firstRow = gi/l.Cols - visibleRows + 1
			}
		}
		cellW := width / l.Cols
		for r := firstRow; r < gridRows && r < firstRow+visibleRows; r++ {
			var cells []string
			for c := 0; c < l.Cols; c++ {
				gi := r*l.Cols + c
				if gi >= len(l.Grid) {
					break
				}
				idx := l.Grid[gi]
				cells = append(cells, renderOverviewCell(instances[idx], num(idx), idx == selected, cellW, cellH))
			}
			rendered = append(rendered, lipgloss.JoinHorizontal(lipgloss.Top, cells...))
		}
		if hidden := len(l.Grid) - (firstRow+visibleRows)*l.Cols; hidden > 0 && gridAvail > 0 {
			rendered = append(rendered, overviewDim.Render(fmt.Sprintf(" … ещё сессии ниже (%d)", hidden)))
		}
	}
	if sleepLine != "" {
		rendered = append(rendered, sleepLine)
	}
	out := strings.Split(strings.Join(rendered, "\n"), "\n")
	if len(out) > height-1 {
		// Never taller than the window: keep the top, and the sleeping line last.
		keep := height - 1
		if sleepLine != "" && keep > 1 {
			out = append(out[:keep-1], sleepLine)
		} else {
			out = out[:keep]
		}
	}
	return header + "\n" + strings.Join(out, "\n")
}
