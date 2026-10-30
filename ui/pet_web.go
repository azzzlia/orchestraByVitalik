package ui

import (
	"fmt"
	"time"
)

// WebPet is a pet for the web interface: its standing and walking frames (braille
// rows, the shared blank rows cut so the feet stand on the bottom), the phrase and
// the caption as plain text. The page animates it by itself.
type WebPet struct {
	Kind    string     `json:"kind"`
	Speaker string     `json:"speaker"`
	Mood    int        `json:"mood"`
	Stand   [][]string `json:"stand"`
	Walk    [][]string `json:"walk,omitempty"`
	StandL  [][]string `json:"stand_l"` // the same, facing left
	WalkL   [][]string `json:"walk_l,omitempty"`
	Phrase  string     `json:"phrase,omitempty"`
	Alert   bool       `json:"alert,omitempty"` // a sad mood: the phrase is shown in red
}

// WebPetInfo is what the page shows under the pets.
type WebPetInfo struct {
	Pets    []WebPet `json:"pets"`
	Caption []string `json:"caption"`
	Warn    string   `json:"warn,omitempty"`
}

// PetsForWeb returns the pets cfg shows (all four in the "all" mode), nil when off.
func PetsForWeb(cfg PetConfig, now time.Time) *WebPetInfo {
	if cfg.Kind == "off" {
		return nil
	}
	mood := 6
	if cfg.HasValue {
		mood = PetMood(cfg.Density)
	}
	kinds := []string{cfg.Kind}
	switch cfg.Kind {
	case "all":
		kinds = AllPets
	case "", "custom":
		kinds = []string{"tyan"}
	}
	info := &WebPetInfo{}
	for _, kind := range kinds {
		var stand, walk [][]string
		if set := chibiFrames[kind]; len(set) > mood {
			stand = set[mood]
		}
		if set := chibiWalk[kind]; len(set) > mood {
			walk = set[mood]
		}
		if len(stand) == 0 {
			continue
		}
		top, bottom := blankEdges(append(append([][]string{}, stand...), walk...))
		p := WebPet{Kind: kind, Speaker: petSpeaker(kind), Mood: mood, Alert: mood <= 3}
		for _, f := range stand {
			// Rows have their trailing blanks cut: pad them to one width first, or the
			// mirrored (left-facing) frame shifts every row by a different amount.
			c := padSprite(cutRows(f, top, bottom))
			p.Stand, p.StandL = append(p.Stand, c), append(p.StandL, mirrorSprite(c))
		}
		for _, f := range walk {
			c := padSprite(cutRows(f, top, bottom))
			p.Walk, p.WalkL = append(p.Walk, c), append(p.WalkL, mirrorSprite(c))
		}
		if cfg.HasValue {
			p.Phrase = PetPhraseFor(kind, mood, now, cfg.Phrases)
		}
		info.Pets = append(info.Pets, p)
	}
	caption := "плотность: пока не известна"
	if cfg.HasValue {
		caption = fmt.Sprintf("плотность %s ч", FormatDensity(cfg.Density))
	}
	if cfg.HasT39 {
		caption += " · t39 " + FormatDensity(cfg.T39)
	}
	info.Caption = append(info.Caption, caption)
	line := goalLine(cfg)
	switch cfg.HourCommit {
	case 1:
		line = joinDot(line, "этот час ✓")
	case -1:
		line = joinDot(line, "этот час ✗")
	}
	if line != "" {
		info.Caption = append(info.Caption, line)
	}
	if cfg.HourEmpty {
		info.Warn = "⚠ час пустой — есть что закоммитить?"
	}
	return info
}

func joinDot(a, b string) string {
	if a == "" {
		return b
	}
	return a + " · " + b
}
