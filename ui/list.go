package ui

import (
	"claude-squad/log"
	"claude-squad/session"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

const readyIcon = "● "
const pausedIcon = "⏸ "

var readyStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#51bd73", Dark: "#51bd73"})

var addedLinesStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#51bd73", Dark: "#51bd73"})

var removedLinesStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#de613e"))

var pausedStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#888888", Dark: "#888888"})

var titleStyle = lipgloss.NewStyle().
	Padding(1, 1, 0, 1).
	Foreground(lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#dddddd"})

var listDescStyle = lipgloss.NewStyle().
	Padding(0, 1, 1, 1).
	Foreground(lipgloss.AdaptiveColor{Light: "#A49FA5", Dark: "#777777"})

var selectedTitleStyle = lipgloss.NewStyle().
	Padding(1, 1, 0, 1).
	Background(lipgloss.Color("#dde4f0")).
	Foreground(lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#1a1a1a"})

var selectedDescStyle = lipgloss.NewStyle().
	Padding(0, 1, 1, 1).
	Background(lipgloss.Color("#dde4f0")).
	Foreground(lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#1a1a1a"})

var mainTitle = lipgloss.NewStyle().
	Background(lipgloss.Color("62")).
	Foreground(lipgloss.Color("230"))

var autoYesStyle = lipgloss.NewStyle().
	Background(lipgloss.Color("#dde4f0")).
	Foreground(lipgloss.Color("#1a1a1a"))

var bannerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#9ca3af"))

type List struct {
	// scrollOffset is the index of the first item shown when the list is taller than the screen.
	scrollOffset  int
	items         []*session.Instance
	selectedIdx   int
	height, width int
	renderer      *InstanceRenderer
	autoyes       bool
	banner        string

	// map of repo name to number of instances using it. Used to display the repo name only if there are
	// multiple repos in play.
	repos map[string]int
}

func NewList(spinner *spinner.Model, autoYes bool) *List {
	return &List{
		items:    []*session.Instance{},
		renderer: &InstanceRenderer{spinner: spinner},
		repos:    make(map[string]int),
		autoyes:  autoYes,
	}
}

// SetSize sets the height and width of the list.
func (l *List) SetSize(width, height int) {
	l.width = width
	l.height = height
	l.renderer.setWidth(width)
}

// SetSessionPreviewSize sets the height and width for the tmux sessions. This makes the stdout line have the correct
// width and height.
func (l *List) SetSessionPreviewSize(width, height int) (err error) {
	for i, item := range l.items {
		if !item.Started() || item.Paused() {
			continue
		}

		if innerErr := item.SetPreviewSize(width, height); innerErr != nil {
			err = errors.Join(
				err, fmt.Errorf("could not set preview size for instance %d: %v", i, innerErr))
		}
	}
	return
}

func (l *List) NumInstances() int {
	return len(l.items)
}

// InstanceRenderer handles rendering of session.Instance objects
type InstanceRenderer struct {
	spinner *spinner.Model
	width   int
}

func (r *InstanceRenderer) setWidth(width int) {
	r.width = AdjustPreviewWidth(width)
}

// ɹ and ɻ are other options.
const branchIcon = "Ꮧ"

// sessionColorHex maps color label names (session.SessionColors) to terminal colors.
var sessionColorHex = map[string]string{
	"red":    "#ef4444",
	"orange": "#f97316",
	"yellow": "#eab308",
	"green":  "#22c55e",
	"cyan":   "#06b6d4",
	"blue":   "#3b82f6",
	"purple": "#a855f7",
	"pink":   "#ec4899",
}

// statusColors maps session status kinds to colors used in the list and overview.
var statusColors = map[session.StatusKind]lipgloss.Color{
	session.StatusWorking:    lipgloss.Color("#22c55e"),
	session.StatusWaiting:    lipgloss.Color("#eab308"),
	session.StatusNeedsInput: lipgloss.Color("#ef4444"),
	session.StatusOnHold:     lipgloss.Color("#38bdf8"),
	session.StatusConductor:  lipgloss.Color("#a78bfa"),
	session.StatusStandby:    lipgloss.Color("#94a3b8"),
	session.StatusPaused:     lipgloss.Color("#6b7280"),
	session.StatusStarting:   lipgloss.Color("#06b6d4"),
}

// StatusText renders "в работе · ждёт ревью" with the status colored.
func StatusText(i *session.Instance, bg lipgloss.TerminalColor) (styled string, plainWidth int) {
	label, kind := i.StatusLabel()
	plain := label
	st := lipgloss.NewStyle().Foreground(statusColors[kind]).Background(bg).Bold(true)
	if i.CallsUserNow() {
		st = st.Blink(true) // the conductor/assistant really needs you: hard to miss
	}
	styled = st.Render(label)
	if stage := i.Stage(); stage != "" {
		sep := " · " + stage
		plain += sep
		styled += lipgloss.NewStyle().Background(bg).Render(sep)
	}
	return styled, runewidth.StringWidth(plain)
}

func (r *InstanceRenderer) Render(i *session.Instance, idx int, selected bool, hasMultipleRepos bool) string {
	if i.Number > 0 {
		idx = i.Number
	}
	prefix := fmt.Sprintf(" %d. ", idx)
	if idx >= 10 {
		prefix = prefix[:len(prefix)-1]
	}
	titleS := selectedTitleStyle
	descS := selectedDescStyle
	if !selected {
		titleS = titleStyle
		descS = listDescStyle
	}

	// add spinner next to title if it's running
	var join string
	switch i.Status {
	case session.Running, session.Loading:
		join = fmt.Sprintf("%s ", r.spinner.View())
	case session.Ready:
		join = readyStyle.Render(readyIcon)
	case session.Paused:
		join = pausedStyle.Render(pausedIcon)
	default:
	}

	// Cut the title if it's too long
	titleText := i.Title
	widthAvail := r.width - 3 - runewidth.StringWidth(prefix) - 1
	if widthAvail > 0 && runewidth.StringWidth(titleText) > widthAvail {
		titleText = runewidth.Truncate(titleText, widthAvail-3, "...")
	}
	titleLine := fmt.Sprintf("%s %s", prefix, titleText)
	if hex, ok := sessionColorHex[i.Color]; ok {
		// Replace the leading space with a colored marker so the layout width is unchanged.
		marker := lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Background(titleS.GetBackground()).Bold(true).Render("●")
		titleLine = marker + strings.TrimPrefix(titleLine, " ")
	}
	title := titleS.Render(lipgloss.JoinHorizontal(
		lipgloss.Left,
		lipgloss.Place(r.width-3, 1, lipgloss.Left, lipgloss.Center, titleLine),
		" ",
		join,
	))

	stat := i.GetDiffStats()

	var diff string
	var addedDiff, removedDiff string
	if stat == nil || stat.Error != nil || stat.IsEmpty() {
		// Don't show diff stats if there's an error or if they don't exist
		addedDiff = ""
		removedDiff = ""
		diff = ""
	} else {
		addedDiff = fmt.Sprintf("+%d", stat.Added)
		removedDiff = fmt.Sprintf("-%d ", stat.Removed)
		diff = lipgloss.JoinHorizontal(
			lipgloss.Center,
			addedLinesStyle.Background(descS.GetBackground()).Render(addedDiff),
			lipgloss.Style{}.Background(descS.GetBackground()).Foreground(descS.GetForeground()).Render(","),
			removedLinesStyle.Background(descS.GetBackground()).Render(removedDiff),
		)
	}

	remainingWidth := r.width
	remainingWidth -= runewidth.StringWidth(prefix)
	remainingWidth -= runewidth.StringWidth(branchIcon)
	remainingWidth -= 2 // for the literal " " and "-" in the branchLine format string

	diffWidth := runewidth.StringWidth(addedDiff) + runewidth.StringWidth(removedDiff)
	if diffWidth > 0 {
		diffWidth += 1
	}

	// Use fixed width for diff stats to avoid layout issues
	remainingWidth -= diffWidth

	branch := i.Branch
	if i.Started() && hasMultipleRepos {
		repoName, err := i.RepoName()
		if err != nil {
			log.ErrorLog.Printf("could not get repo name in instance renderer: %v", err)
		} else {
			branch += fmt.Sprintf(" (%s)", repoName)
		}
	}
	// Don't show branch if there's no space for it. Or show ellipsis if it's too long.
	branchWidth := runewidth.StringWidth(branch)
	if remainingWidth < 0 {
		branch = ""
	} else if remainingWidth < branchWidth {
		if remainingWidth < 3 {
			branch = ""
		} else {
			// We know the remainingWidth is at least 4 and branch is longer than that, so this is safe.
			branch = runewidth.Truncate(branch, remainingWidth-3, "...")
		}
	}
	remainingWidth -= runewidth.StringWidth(branch)

	// Add spaces to fill the remaining width.
	spaces := ""
	if remainingWidth > 0 {
		spaces = strings.Repeat(" ", remainingWidth)
	}

	branchLine := fmt.Sprintf("%s %s-%s%s%s", strings.Repeat(" ", len(prefix)), branchIcon, branch, spaces, diff)

	// join title and subtitle
	desc := branchLine
	if i.Conductor {
		desc += "\n" + strings.Repeat(" ", len(prefix)+1) + "♛ дирижёр всех сессий"
	} else if i.Assistant {
		desc += "\n" + strings.Repeat(" ", len(prefix)+1) + "✦ помощник — твои вопросы"
	} else if folder := i.FolderLabel(); folder != "" {
		indent := strings.Repeat(" ", len(prefix)+1)
		usage := ""
		if i.UsageToday >= 1000 {
			usage = " · " + session.FormatUnits(i.UsageToday) + " сегодня"
		}
		maxW := r.width - runewidth.StringWidth(indent) - 3 - runewidth.StringWidth(usage)
		if maxW > 3 && runewidth.StringWidth(folder) > maxW {
			folder = runewidth.Truncate(folder, maxW-1, "…")
		}
		folder += usage
		desc += "\n" + indent + "▸ " + folder
	}
	if statusStyled, sw := StatusText(i, descS.GetBackground()); statusStyled != "" {
		indent := strings.Repeat(" ", len(prefix)+1)
		// A long status ("нужно разрешение · <the whole command>") must not widen the
		// list: that pushed the preview off the screen to the right.
		if w := r.width - runewidth.StringWidth(indent) - 1; w > 3 && sw > w {
			statusStyled = ansi.Truncate(statusStyled, w-1, "…")
		}
		desc += "\n" + indent + statusStyled
	}
	if i.WipedText != "" {
		indent := strings.Repeat(" ", len(prefix)+1)
		line := "✂ стёрт забытый черновик «" + i.WipedText + "»"
		if w := r.width - runewidth.StringWidth(indent) - 1; w > 3 && runewidth.StringWidth(line) > w {
			line = runewidth.Truncate(line, w-1, "…")
		}
		desc += "\n" + indent + line
	}
	if note := strings.TrimSpace(i.Note); note != "" {
		indent := strings.Repeat(" ", len(prefix)+1)
		noteWidth := r.width - runewidth.StringWidth(indent) - 1
		if noteWidth > 3 {
			if runewidth.StringWidth(note) > noteWidth {
				note = runewidth.Truncate(note, noteWidth-3, "...")
			}
			desc += "\n" + indent + note
		}
	}
	text := lipgloss.JoinVertical(lipgloss.Left, title, descS.Render(desc))

	return text
}

// SetBanner sets the text shown on the right of the title (limits, VPN).
func (l *List) SetBanner(s string) { l.banner = s }

func (l *List) String() string {
	const titleText = " orchestraByVitalik "
	const autoYesText = " auto-yes "

	// Write the title.
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("\n")

	// Write title line
	// add padding of 2 because the border on list items adds some extra characters
	titleWidth := AdjustPreviewWidth(l.width) + 2
	if l.banner != "" && !l.autoyes {
		title := lipgloss.Place(
			titleWidth/2, 1, lipgloss.Left, lipgloss.Bottom, mainTitle.Render(titleText))
		banner := lipgloss.Place(
			titleWidth-(titleWidth/2), 1, lipgloss.Right, lipgloss.Bottom, bannerStyle.Render(" "+l.banner+" "))
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, title, banner))
	} else if !l.autoyes {
		b.WriteString(lipgloss.Place(
			titleWidth, 1, lipgloss.Left, lipgloss.Bottom, mainTitle.Render(titleText)))
	} else {
		title := lipgloss.Place(
			titleWidth/2, 1, lipgloss.Left, lipgloss.Bottom, mainTitle.Render(titleText))
		autoYes := lipgloss.Place(
			titleWidth-(titleWidth/2), 1, lipgloss.Right, lipgloss.Bottom, autoYesStyle.Render(autoYesText))
		b.WriteString(lipgloss.JoinHorizontal(
			lipgloss.Top, title, autoYes))
	}

	b.WriteString("\n")
	b.WriteString("\n")

	// Render the list, scrolled so the selected item is always visible.
	header := b.String()
	avail := l.height - strings.Count(header, "\n") - 1
	blocks := make([]string, len(l.items))
	heights := make([]int, len(l.items))
	for i, item := range l.items {
		blocks[i] = l.renderer.Render(item, i+1, i == l.selectedIdx, len(l.repos) > 1)
		heights[i] = strings.Count(blocks[i], "\n") + 2 // block lines + blank separator
	}
	total := 0
	for _, h := range heights {
		total += h
	}
	start, end := 0, len(blocks)
	if total > avail && len(blocks) > 0 {
		avail -= 2 // room for the "more above/below" hints
		if l.scrollOffset > l.selectedIdx {
			l.scrollOffset = l.selectedIdx
		}
		sum := func(from, to int) int {
			n := 0
			for i := from; i <= to && i < len(heights); i++ {
				n += heights[i]
			}
			return n
		}
		for l.scrollOffset < l.selectedIdx && sum(l.scrollOffset, l.selectedIdx) > avail {
			l.scrollOffset++
		}
		start = l.scrollOffset
		used := 0
		end = start
		for end < len(blocks) && (end == start || used+heights[end] <= avail) {
			used += heights[end]
			end++
		}
	} else {
		l.scrollOffset = 0
	}
	if start > 0 {
		b.WriteString(listDescStyle.Render(fmt.Sprintf("  ↑ ещё %d", start)) + "\n")
	}
	for i := start; i < end; i++ {
		b.WriteString(blocks[i])
		if i != end-1 {
			b.WriteString("\n\n")
		}
	}
	if end < len(blocks) {
		b.WriteString("\n\n" + listDescStyle.Render(fmt.Sprintf("  ↓ ещё %d", len(blocks)-end)))
	}
	return lipgloss.Place(l.width, l.height, lipgloss.Left, lipgloss.Top, b.String())
}

// Down selects the next item in the list.
func (l *List) Down() {
	if len(l.items) == 0 {
		return
	}
	if l.selectedIdx < len(l.items)-1 {
		l.selectedIdx++
	} else {
		l.selectedIdx = 0
	}
}

// Kill selects the next item in the list.
func (l *List) Kill() {
	if len(l.items) == 0 {
		return
	}
	targetInstance := l.items[l.selectedIdx]

	// Kill the tmux session
	if err := targetInstance.Kill(); err != nil {
		log.ErrorLog.Printf("could not kill instance: %v", err)
	}

	// If you delete the last one in the list, select the previous one.
	if l.selectedIdx == len(l.items)-1 {
		defer l.Up()
	}

	// Unregister the reponame.
	repoName, err := targetInstance.RepoName()
	if err != nil {
		log.ErrorLog.Printf("could not get repo name: %v", err)
	} else {
		l.rmRepo(repoName)
	}

	// Since there's items after this, the selectedIdx can stay the same.
	l.items = append(l.items[:l.selectedIdx], l.items[l.selectedIdx+1:]...)
}

func (l *List) Attach() (chan struct{}, error) {
	targetInstance := l.items[l.selectedIdx]
	return targetInstance.Attach()
}

// Up selects the prev item in the list.
func (l *List) Up() {
	if len(l.items) == 0 {
		return
	}
	if l.selectedIdx > 0 {
		l.selectedIdx--
	} else {
		l.selectedIdx = len(l.items) - 1
	}
}

func (l *List) addRepo(repo string) {
	if _, ok := l.repos[repo]; !ok {
		l.repos[repo] = 0
	}
	l.repos[repo]++
}

func (l *List) rmRepo(repo string) {
	if _, ok := l.repos[repo]; !ok {
		log.ErrorLog.Printf("repo %s not found", repo)
		return
	}
	l.repos[repo]--
	if l.repos[repo] == 0 {
		delete(l.repos, repo)
	}
}

// AddInstance adds a new instance to the list. It returns a finalizer function that should be called when the instance
// is started. If the instance was restored from storage or is paused, you can call the finalizer immediately.
// When creating a new one and entering the name, you want to call the finalizer once the name is done.
func (l *List) AddInstance(instance *session.Instance) (finalize func()) {
	l.items = append(l.items, instance)
	// The finalizer registers the repo name once the instance is started.
	return func() {
		repoName, err := instance.RepoName()
		if err != nil {
			log.ErrorLog.Printf("could not get repo name: %v", err)
			return
		}

		l.addRepo(repoName)
	}
}

// GetSelectedInstance returns the currently selected instance
func (l *List) GetSelectedInstance() *session.Instance {
	if len(l.items) == 0 {
		return nil
	}
	return l.items[l.selectedIdx]
}

// SetSelectedInstance sets the selected index. Noop if the index is out of bounds.
func (l *List) SetSelectedInstance(idx int) {
	if idx >= len(l.items) {
		return
	}
	l.selectedIdx = idx
}

// SelectInstance finds and selects the given instance in the list.
func (l *List) SelectInstance(target *session.Instance) {
	for i, inst := range l.items {
		if inst == target {
			l.SetSelectedInstance(i)
			return
		}
	}
}

// MoveUp swaps the selected instance with the one above it.
func (l *List) MoveUp() bool {
	if l.selectedIdx <= 0 || len(l.items) < 2 {
		return false
	}
	l.items[l.selectedIdx], l.items[l.selectedIdx-1] = l.items[l.selectedIdx-1], l.items[l.selectedIdx]
	l.selectedIdx--
	return true
}

// MoveDown swaps the selected instance with the one below it.
func (l *List) MoveDown() bool {
	if l.selectedIdx >= len(l.items)-1 || len(l.items) < 2 {
		return false
	}
	l.items[l.selectedIdx], l.items[l.selectedIdx+1] = l.items[l.selectedIdx+1], l.items[l.selectedIdx]
	l.selectedIdx++
	return true
}

// GetInstances returns all instances in the list
// Reorder replaces the item order (same items), keeping the selected item selected.
func (l *List) Reorder(sorted []*session.Instance) {
	if len(sorted) != len(l.items) {
		return
	}
	same := true
	for i := range sorted {
		if sorted[i] != l.items[i] {
			same = false
			break
		}
	}
	if same {
		return
	}
	selected := l.GetSelectedInstance()
	l.items = sorted
	if selected != nil {
		l.SelectInstance(selected)
	}
}

func (l *List) GetInstances() []*session.Instance {
	return l.items
}
