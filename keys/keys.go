package keys

import (
	"github.com/charmbracelet/bubbles/key"
)

type KeyName int

const (
	KeyUp KeyName = iota
	KeyDown
	KeyEnter
	KeyNew
	KeyKill
	KeyQuit
	KeyReview
	KeyPush
	KeySubmit

	KeyTab        // Tab is a special keybinding for switching between panes.
	KeySubmitName // SubmitName is a special keybinding for submitting the name of a new instance.

	KeyCheckout
	KeyResume
	KeyPrompt // New key for entering a prompt
	KeyHelp   // Key for showing help screen

	// Diff keybindings
	KeyShiftUp
	KeyShiftDown

	// Reorder keybindings
	KeyMoveUp
	KeyMoveDown

	KeyColor    // cycle the session's color label
	KeyNote     // edit the session's description
	KeyReply    // send a message to the selected session without attaching
	KeyOverview // show all sessions at once
	KeySummary  // copy a status summary of all sessions
	KeyRename   // rename the selected session

	KeyWhatHappened // what happened while the user was away
	KeyOpenReport   // open the selected session's QA report
	KeyOpenFolder   // open the selected session's folder
	KeyQueue        // «ждут тебя»: every session waiting for the user
)

// GlobalKeyStringsMap is a global, immutable map string to keybinding.
var GlobalKeyStringsMap = map[string]KeyName{
	"up":         KeyUp,
	"k":          KeyUp,
	"down":       KeyDown,
	"j":          KeyDown,
	"shift+up":   KeyShiftUp,
	"shift+down": KeyShiftDown,
	"J":          KeyMoveDown,
	"K":          KeyMoveUp,
	"N":          KeyPrompt,
	"enter":      KeyEnter,
	"o":          KeyEnter,
	"n":          KeyNew,
	"D":          KeyKill,
	"q":          KeyQuit,
	"tab":        KeyTab,
	"c":          KeyCheckout,
	"r":          KeyResume,
	"p":          KeySubmit,
	"?":          KeyHelp,
	"C":          KeyColor,
	"e":          KeyNote,
	"i":          KeyReply,
	"v":          KeyOverview,
	"s":          KeySummary,
	"R":          KeyRename,
	"w":          KeyWhatHappened,
	"O":          KeyOpenReport,
	"f":          KeyOpenFolder,
	"u":          KeyQueue,
}

// GlobalkeyBindings is a global, immutable map of KeyName tot keybinding.
var GlobalkeyBindings = map[KeyName]key.Binding{
	KeyUp: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	KeyDown: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	KeyShiftUp: key.NewBinding(
		key.WithKeys("shift+up"),
		key.WithHelp("shift+↑", "scroll"),
	),
	KeyShiftDown: key.NewBinding(
		key.WithKeys("shift+down"),
		key.WithHelp("shift+↓", "scroll"),
	),
	KeyEnter: key.NewBinding(
		key.WithKeys("enter", "o"),
		key.WithHelp("↵/o", "open"),
	),
	KeyNew: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "new"),
	),
	KeyKill: key.NewBinding(
		key.WithKeys("D"),
		key.WithHelp("D", "kill"),
	),
	KeyHelp: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
	KeyQuit: key.NewBinding(
		key.WithKeys("q"),
		key.WithHelp("q", "quit"),
	),
	KeySubmit: key.NewBinding(
		key.WithKeys("p"),
		key.WithHelp("p", "push branch"),
	),
	KeyPrompt: key.NewBinding(
		key.WithKeys("N"),
		key.WithHelp("N", "new with prompt"),
	),
	KeyCheckout: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "checkout"),
	),
	KeyTab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "switch tab"),
	),
	KeyResume: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "resume"),
	),

	KeyColor: key.NewBinding(
		key.WithKeys("C"),
		key.WithHelp("C", "color"),
	),
	KeyNote: key.NewBinding(
		key.WithKeys("e"),
		key.WithHelp("e", "describe"),
	),
	KeyReply: key.NewBinding(
		key.WithKeys("i"),
		key.WithHelp("i", "reply"),
	),
	KeyOverview: key.NewBinding(
		key.WithKeys("v"),
		key.WithHelp("v", "overview"),
	),
	KeySummary: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "summary"),
	),
	KeyWhatHappened: key.NewBinding(
		key.WithKeys("w"),
		key.WithHelp("w", "что было"),
	),
	KeyOpenReport: key.NewBinding(
		key.WithKeys("O"),
		key.WithHelp("O", "отчёт"),
	),
	KeyQueue: key.NewBinding(
		key.WithKeys("u"),
		key.WithHelp("u", "ждут тебя"),
	),
	KeyOpenFolder: key.NewBinding(
		key.WithKeys("f"),
		key.WithHelp("f", "папка"),
	),
	KeyRename: key.NewBinding(
		key.WithKeys("R"),
		key.WithHelp("R", "rename"),
	),

	KeyMoveUp: key.NewBinding(
		key.WithKeys("K"),
		key.WithHelp("K", "move up"),
	),
	KeyMoveDown: key.NewBinding(
		key.WithKeys("J"),
		key.WithHelp("J", "move down"),
	),

	// -- Special keybindings --

	KeySubmitName: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "submit name"),
	),
}
