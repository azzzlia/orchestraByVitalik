package ui

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// A small text pet in the bottom-right corner of the overview. Its mood follows
// the user's "плотность": how many hours today had a commit (goal — 6). Twelve
// moods from crying (0 hours) to very-very happy (21+). Monochrome braille, animated.

// PetMoodLabels name the twelve moods, saddest first.
var PetMoodLabels = []string{"рыдает", "очень грустно", "грустно", "так себе", "скучает", "почти норм",
	"довольна", "радуется", "очень радуется", "в восторге", "сияет", "очень-очень радостно"}

// petMoodFrom[m] is the lowest density that gives mood m.
var petMoodFrom = []float64{0, 1, 2, 3, 4, 5, 6, 8, 10, 13, 17, 21}

// PetMood maps a density score to a mood 0..11.
func PetMood(density float64) int {
	mood := 0
	for m, from := range petMoodFrom {
		if density >= from {
			mood = m
		}
	}
	return mood
}

// FormatDensity prints 5 as "5" and 10.4 as "10,4".
func FormatDensity(v float64) string {
	s := strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64)
	return strings.Replace(s, ".", ",", 1)
}

// petFrames returns the animation frames of the built-in pet (braille line art).
func petFrames(kind string, mood int) [][]string {
	set := tyanFrames
	switch kind {
	case "os":
		set = osFrames
	case "dog":
		set = dogFrames
	case "cat":
		set = catFrames
	}
	if mood < 0 || mood >= len(set) {
		mood = 6
	}
	return set[mood]
}

// petPhrases are what each pet says about the density, saddest mood first (several
// per mood, they take turns every 10 seconds). Your own: ~/.claude-squad/pet-phrases.json.
var petPhrases = map[string][][]string{
	"os": {
		{"Что там по плотности??", "Ноль часов. Ноль.", "Зайди ко мне."},
		{"Поторопись.", "Где коммиты?", "Это что за темп?"},
		{"Как дела?", "Что-то тихо у тебя.", "Есть блокеры?"},
		{"ТТМ три дня.", "Какой ETA?", "Время идёт."},
		{"Ну такое.", "Можно и бодрее.", "Держи в курсе."},
		{"Почти. Дожимай.", "Ещё часик — и норм.", "Уже лучше."},
		{"Норм. Держи темп.", "Цель взята.", "Вот так и работаем."},
		{"Хорошо идёшь.", "Молодец.", "Так держать."},
		{"Вот это я понимаю!", "Отлично!", "Красавчик."},
		{"На планёрке отмечу.", "Огонь!", "Пример для команды."},
		{"Кейс для всего отдела.", "Премию обсудим.", "Вот это плотность."},
		{"Легенда.", "Иди отдыхай, заслужил.", "Я горжусь."},
	},
	"tyan": {
		{"Ни одного коммита… я плачу", "Ты про меня забыл?", "Дождик… и ноль часов"},
		{"Ну хоть один коммитик?", "Мне грустно без коммитов", "Шмыг…"},
		{"Я скучаю по коммитам", "Грустненько сегодня", "Закоммить хоть что-то"},
		{"Так себе денёк", "Ну-у… можно лучше", "Давай поднажмём?"},
		{"Скучно-о-о", "Я сейчас усну…", "Разбуди меня коммитом"},
		{"Ещё чуть-чуть до нормы!", "Почти-почти!", "Час — и я довольна"},
		{"Цель взята, ты молодец!", "Вот теперь хорошо", "Довольна!"},
		{"Ура, хорошо идём!", "Ты сегодня бодрый", "Хи-хи, отлично"},
		{"Обожаю, когда ты коммитишь!", "Ещё, ещё!", "Какой продуктивный!"},
		{"Ты сегодня в ударе!", "Я танцую!", "Блестяще!"},
		{"Звёздный день!", "Ты звезда!", "Вау-вау-вау"},
		{"Люблю-люблю! ♥", "Иди отдыхай ♥", "Лучший день ♥"},
	},
	"dog": {
		{"Ау-у… ни одного коммита", "Скулит у двери", "Хозяин, ты где?"},
		{"Где коммиты? Гав…", "Принёс тапки, а коммитов нет", "Грустный хвостик"},
		{"Положил морду на лапы", "Ждёт коммит, как прогулку", "Тихо поскуливает"},
		{"Ну кинь хоть палочку-коммит", "Так себе прогулка", "Гав?"},
		{"Зевает… кинь коммит, как мячик", "Спит у ног", "Скучно, гав"},
		{"Виляет хвостом: почти!", "Ещё чуть-чуть — и гулять!", "Уши торчком"},
		{"Гав! Цель взята!", "Хороший хозяин", "Довольно пыхтит"},
		{"Принёс палку за хороший час", "Виляет всем телом", "Гав-гав!"},
		{"Носится кругами от радости", "Прыгает на ручки", "Ура, гулять!"},
		{"Лучший хозяин на свете!", "Хвост — пропеллер", "Гав-гав-гав!"},
		{"Счастлив до звёзд!", "Сносит всё хвостом", "ГАВ!!!"},
		{"Лижет тебе лицо ♥", "Самый любимый ♥", "Уснул счастливым ♥"},
	},
	"cat": {
		{"Мяу. Ни одного коммита. Я ухожу", "Демонстративно отвернулся", "Мокнет под дождём"},
		{"Смотрит с осуждением", "Где мои коммиты, человек?", "Фыр"},
		{"Скинул кружку со стола", "Недовольно бьёт хвостом", "Мрр… нет"},
		{"Ну такое… мур", "Так себе, человек", "Щурится"},
		{"Спит на клавиатуре", "Зевает: скукота", "Свернулся клубком"},
		{"Почти норм, можно погладить", "Приоткрыл один глаз", "Мур?"},
		{"Мур. Цель взята", "Сидит довольный", "Неплохо, человек"},
		{"Мурчит на коленях", "Трётся о монитор", "Мррр"},
		{"Принёс тебе мышку", "Мур-мур-мур", "Топчет лапками"},
		{"Трётся об ноги", "Громко мурчит", "Хороший человек"},
		{"Носится по квартире", "Мяу-мяу-мяу!", "Звёздный кот"},
		{"Признал тебя своим человеком ♥", "Спит у тебя на груди ♥", "Мурчит от любви ♥"},
	},
}

// PetPhrases are your own phrases: kind → mood ("0".."11") → lines. Moods you leave out
// keep the built-in ones.
type PetPhrases map[string]map[string][]string

// LoadPetPhrases reads your phrases from a JSON file (nil when it is missing or broken).
func LoadPetPhrases(path string) PetPhrases {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var p PetPhrases
	if json.Unmarshal(data, &p) != nil {
		return nil
	}
	return p
}

// PetPhraseFor is the pet's line for a mood; lines take turns every 10 seconds.
func PetPhraseFor(kind string, mood int, now time.Time, own PetPhrases) string {
	if mood < 0 || mood >= len(PetMoodLabels) {
		mood = 6
	}
	if lines := own[kind][strconv.Itoa(mood)]; len(lines) > 0 {
		return lines[int(now.Unix()/10)%len(lines)]
	}
	set := petPhrases[kind]
	if len(set) <= mood || len(set[mood]) == 0 {
		return ""
	}
	return set[mood][int(now.Unix()/10)%len(set[mood])]
}

// PetPhrase is the manager pet's line (kept for the status line).
func PetPhrase(mood int, now time.Time) string { return PetPhraseFor("os", mood, now, nil) }

// PetKinds are the pets the overview cycles through with p.
func PetKinds(hasCustom bool) []string {
	if hasCustom {
		return []string{"tyan", "os", "dog", "cat", "all", "custom", "off"}
	}
	return []string{"tyan", "os", "dog", "cat", "all", "off"}
}

// NextPet is the pet after cur in PetKinds ("" counts as tyan).
func NextPet(cur string, hasCustom bool) string {
	kinds := PetKinds(hasCustom)
	if cur == "" {
		cur = "tyan"
	}
	for i, k := range kinds {
		if k == cur {
			return kinds[(i+1)%len(kinds)]
		}
	}
	return kinds[0]
}

// petSpeaker is the name before the pet's phrase.
func petSpeaker(kind string) string {
	switch kind {
	case "os":
		return "ОС"
	case "dog":
		return "Щенок"
	case "cat":
		return "Котик"
	}
	return "Тян"
}

// PetName is how a pet is called in the UI.
func PetName(kind string) string {
	switch kind {
	case "os":
		return "ОС"
	case "dog":
		return "щенок"
	case "cat":
		return "котик"
	case "all":
		return "все сразу"
	case "custom":
		return "свой"
	case "off":
		return "без питомца"
	}
	return "тян"
}

var customCache = map[string]struct {
	at     time.Time
	frames [][]string
}{}

// customPetFrames reads <dir>/<mood 1..12>.txt (cached for 5 s: it is called on
// every frame); frames are separated by a "---" line.
func customPetFrames(dir string, mood int) [][]string {
	key := fmt.Sprintf("%s#%d", dir, mood)
	if c, ok := customCache[key]; ok && time.Since(c.at) < 5*time.Second {
		return c.frames
	}
	f := readCustomPetFrames(dir, mood)
	customCache[key] = struct {
		at     time.Time
		frames [][]string
	}{time.Now(), f}
	return f
}

func readCustomPetFrames(dir string, mood int) [][]string {
	data, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("%d.txt", mood+1)))
	if err != nil {
		return nil
	}
	var frames [][]string
	for _, part := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n---\n") {
		lines := strings.Split(strings.TrimRight(part, "\n"), "\n")
		if len(lines) > 8 {
			lines = lines[:8]
		}
		frames = append(frames, lines)
	}
	return frames
}

var petStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#6b7280", Dark: "#9ca3af"})
var petAlertStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#eab308"))

// goalLine is "до цели 2 ч" or "цель взята ✓" ("" without a goal).
func goalLine(cfg PetConfig) string {
	if cfg.Goal <= 0 || !cfg.HasValue {
		return ""
	}
	if cfg.Density >= cfg.Goal {
		return "цель взята ✓"
	}
	return "до цели " + FormatDensity(cfg.Goal-cfg.Density) + " ч"
}

var petDoneStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e")).Bold(true)

// hourMark is ✓ (green) when this hour already has a commit, ✗ when not yet.
func hourMark(v int) string {
	switch v {
	case 1:
		return petDoneStyle.Render("✓")
	case -1:
		return petCaptionStyle.Render("✗")
	}
	return ""
}

var petCaptionStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#9ca3af", Dark: "#6b7280"})

// PetConfig says which pet to show and the density score it reacts to.
type PetConfig struct {
	Kind      string // tyan (default) | os | dog | cat | custom | off
	CustomDir string
	Density   float64
	HasValue  bool
	Goal      float64 // daily goal in hours (0 — no goal line)
	HourEmpty bool    // the working hour is ending without a commit
	// HourCommit: there is a commit this hour (1), not yet (-1), unknown (0).
	// Shown as "этот час ✓" next to the goal.
	HourCommit int
	T39        float64 // the official score from t39, when known
	HasT39     bool
	Big        bool       // the old big portrait instead of the small full-height pet
	Phrases    PetPhrases // your own phrases (pet-phrases.json)
}

// RenderPet returns the pet's current animation frame with its caption, or "".
func RenderPet(cfg PetConfig, now time.Time) string {
	p := petParts(cfg, now)
	if len(p.Sprite) == 0 {
		return ""
	}
	var b strings.Builder
	if p.Speech != "" {
		b.WriteString(p.Speech)
		b.WriteByte('\n')
	}
	for _, l := range p.Sprite {
		b.WriteString(petStyle.Render(l))
		b.WriteByte('\n')
	}
	b.WriteString(strings.Join(p.Caption, "\n"))
	return b.String()
}

// PetParts is the pet split for drawing: the phrase above it, the braille sprite
// (plain text, blank braille is see-through) and the caption lines under it.
type PetParts struct {
	Speech  string
	Sprite  []string
	Caption []string
}

func petParts(cfg PetConfig, now time.Time) PetParts { return petPartsAt(cfg, now, false) }

// petPartsAt: walking — the pet is on the move (legs step instead of the mood animation).
func petPartsAt(cfg PetConfig, now time.Time, walking bool) PetParts {
	var p PetParts
	if cfg.Kind == "off" {
		return p
	}
	mood := 6
	if cfg.HasValue {
		mood = PetMood(cfg.Density)
	}
	var frames [][]string
	if cfg.Kind == "custom" {
		frames = customPetFrames(cfg.CustomDir, mood)
	}
	if len(frames) == 0 && !cfg.Big {
		kind := cfg.Kind
		if kind == "" || kind == "custom" || kind == "all" {
			kind = "tyan"
		}
		if set := chibiFrames[kind]; len(set) > mood {
			frames = set[mood]
		}
	}
	if len(frames) == 0 {
		frames = petFrames(cfg.Kind, mood)
	}
	frame := frames[int(now.UnixMilli()/700)%len(frames)]
	if !cfg.Big && cfg.Kind != "custom" {
		kind := cfg.Kind
		if kind == "" || kind == "all" {
			kind = "tyan"
		}
		walk := chibiWalk[kind]
		var steps [][]string
		if len(walk) > mood {
			steps = walk[mood]
		}
		// Cut the blank rows all frames share, so the feet stand on the bottom line
		// and do not jump when the pet switches between standing and walking.
		top, bottom := blankEdges(append(append([][]string{}, frames...), steps...))
		if walking && len(steps) > 0 {
			frame = steps[int(now.UnixMilli()/220)%len(steps)]
		}
		frame = cutRows(frame, top, bottom)
	}
	p.Sprite = frame
	caption := "плотность: пока не известна"
	if cfg.HasValue {
		caption = fmt.Sprintf("плотность %s ч", FormatDensity(cfg.Density))
		if cfg.Big && cfg.Kind != "os" {
			label := PetMoodLabels[mood]
			if (cfg.Kind == "dog" || cfg.Kind == "cat") && label == "довольна" {
				label = "доволен"
			}
			caption += " · " + label
		}
	}
	if cfg.HasT39 {
		caption += " · t39 " + FormatDensity(cfg.T39)
	}
	kind := cfg.Kind
	if kind == "" || kind == "custom" || kind == "all" {
		kind = "tyan"
	}
	if phrase := PetPhraseFor(kind, mood, now, cfg.Phrases); phrase != "" && cfg.HasValue {
		st := petCaptionStyle
		if mood <= 3 {
			st = petAlertStyle
		}
		p.Speech = st.Render(petSpeaker(kind) + ": «" + phrase + "»")
	}
	p.Caption = append(p.Caption, petCaptionStyle.Render(caption))
	line := goalLine(cfg)
	if mark := hourMark(cfg.HourCommit); mark != "" {
		if line != "" {
			line += " · "
		}
		p.Caption = append(p.Caption, petCaptionStyle.Render(line+"этот час ")+mark)
	} else if line != "" {
		p.Caption = append(p.Caption, petCaptionStyle.Render(line))
	}
	if cfg.HourEmpty {
		p.Caption = append(p.Caption, petAlertStyle.Render("⚠ час пустой — есть что закоммитить?"))
	}
	return p
}

// OverlayPet draws the pet over bg (width × height) without hiding what is under
// it: only the dots of the sprite are drawn, blank cells stay see-through. The
// caption sits in the bottom-right corner; the pet walks along the bottom of the
// screen (still — stands next to the caption), its phrase floating above its head.
func OverlayPet(bg string, cfg PetConfig, still bool, width, height int, now time.Time) string {
	kinds := []string{cfg.Kind}
	if cfg.Kind == "all" {
		kinds = AllPets
	}
	first := cfg
	first.Kind = kinds[0]
	probe := petParts(first, now)
	if len(probe.Sprite) == 0 {
		return bg
	}
	lines := strings.Split(bg, "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	capW := 0
	for _, c := range probe.Caption {
		if w := lipgloss.Width(c); w > capW {
			capW = w
		}
	}
	spriteW := len([]rune(padSprite(probe.Sprite)[0]))
	if len(kinds) > 1 {
		// Lanes as wide as the widest pet, or the wider ones would step on a neighbour.
		for _, k := range kinds {
			if set := chibiFrames[k]; len(set) > 0 && len(set[0]) > 0 {
				if w := len([]rune(padSprite(set[0][0])[0])); w+1 > spriteW {
					spriteW = w + 1
				}
			}
		}
	}
	if spriteW+capW+3 > width || len(probe.Sprite)+1 > height {
		return bg
	}
	// Caption: bottom-right, solid (it is text to read).
	for k, c := range probe.Caption {
		row := height - len(probe.Caption) + k
		lines[row] = overlayAt(lines[row], width-capW-1, c, width)
	}
	// Pets walk along the bottom line, left of the caption. With several, each has its
	// own route, and they take turns to speak (one phrase at a time, every 10 s).
	walkMax := width - capW - spriteW - 3
	speaker := int(now.Unix()/10) % len(kinds)
	// Several pets never step on each other: the bottom line is split into lanes, one
	// per pet, and each walks only inside its own.
	area := walkMax + spriteW
	lane := area / len(kinds)
	for n, kind := range kinds {
		c := cfg
		c.Kind = kind
		start, laneMax := 0, walkMax
		if len(kinds) > 1 {
			start, laneMax = n*lane, lane-spriteW-1
		}
		x, faceLeft, moving := start+laneMax, false, false
		if laneMax < 0 {
			x = start // too narrow to walk: stand side by side
		} else if !still {
			var dx int
			dx, faceLeft, moving = petWalk(now.Add(time.Duration(n)*7300*time.Millisecond), laneMax)
			x = start + dx
		}
		p := petPartsAt(c, now, moving)
		sprite := padSprite(p.Sprite)
		if len(sprite) == 0 || len(sprite)+1 > height {
			continue
		}
		sw := len([]rune(sprite[0]))
		top := height - len(sprite)
		if x < 0 {
			x = 0
		}
		if faceLeft {
			sprite = mirrorSprite(sprite)
		}
		for k, row := range sprite {
			lines[top+k] = overlayDots(lines[top+k], x, row, width)
		}
		if p.Speech != "" && top > 0 && n == speaker {
			sx := x + sw/2 - lipgloss.Width(p.Speech)/2
			if max := width - lipgloss.Width(p.Speech) - 1; sx > max {
				sx = max
			}
			if sx < 0 {
				sx = 0
			}
			lines[top-1] = overlayAt(lines[top-1], sx, p.Speech, width)
		}
	}
	return strings.Join(lines, "\n")
}

// AllPets are the pets shown together in the "all" mode.
var AllPets = []string{"tyan", "os", "dog", "cat"}

// petWalk: where the pet is (column), whether it faces left and whether it is
// walking right now. Every 15 s it picks a new spot and walks there at ~7 columns
// a second, then stands.
func petWalk(now time.Time, maxX int) (int, bool, bool) {
	if maxX <= 0 {
		return 0, false, false
	}
	const seg = 15000
	ms := now.UnixMilli()
	k := ms / seg
	spot := func(k int64) int {
		h := uint64(k)*0x9E3779B97F4A7C15 + 0x632BE59BD9B4E019
		h ^= h >> 31
		return int(h % uint64(maxX+1))
	}
	from, to := spot(k-1), spot(k)
	walked := int((ms % seg) / 140)
	d := to - from
	left := d < 0
	if left {
		d = -d
	}
	moving := walked < d
	if !moving {
		walked = d
	}
	if left {
		return from - walked, true, moving
	}
	return from + walked, false, moving
}

// blankEdges: how many rows at the top and at the bottom are blank in every frame.
func blankEdges(frames [][]string) (top, bottom int) {
	blank := func(r string) bool { return strings.Trim(r, "\u2800 ") == "" }
	top, bottom = 1<<30, 1<<30
	for _, f := range frames {
		t := 0
		for t < len(f) && blank(f[t]) {
			t++
		}
		b := 0
		for b < len(f)-t && blank(f[len(f)-1-b]) {
			b++
		}
		if t < top {
			top = t
		}
		if b < bottom {
			bottom = b
		}
	}
	if top == 1<<30 {
		return 0, 0
	}
	return top, bottom
}

func cutRows(f []string, top, bottom int) []string {
	if top+bottom >= len(f) {
		return f
	}
	return f[top : len(f)-bottom]
}

// padSprite makes all sprite rows the same width (blank braille on the right).
func padSprite(rows []string) []string {
	w := 0
	for _, r := range rows {
		if n := len([]rune(r)); n > w {
			w = n
		}
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r + strings.Repeat("⠀", w-len([]rune(r)))
	}
	return out
}

// mirrorSprite flips braille art left-to-right.
func mirrorSprite(rows []string) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		rs := []rune(r)
		for a, b := 0, len(rs)-1; a < b; a, b = a+1, b-1 {
			rs[a], rs[b] = rs[b], rs[a]
		}
		for j, c := range rs {
			if c >= 0x2800 && c <= 0x28FF {
				v := c - 0x2800
				l := v & 0x47  // dots 1,2,3,7
				rr := v & 0xB8 // dots 4,5,6,8
				m := (l&0x07)<<3 | (l&0x40)<<1 | (rr&0x38)>>3 | (rr&0x80)>>1
				rs[j] = 0x2800 + m
			}
		}
		out[i] = string(rs)
	}
	return out
}

// overlayAt puts fg over line from column x on (fg is solid).
func overlayAt(line string, x int, fg string, width int) string {
	w := lipgloss.Width(fg)
	if x < 0 || x+w > width {
		return line
	}
	base := ansi.Truncate(line, x, "")
	if bw := lipgloss.Width(base); bw < x {
		base += strings.Repeat(" ", x-bw)
	}
	out := base + "\x1b[0m" + fg + "\x1b[0m" + ansi.TruncateLeft(line, x+w, "")
	if lipgloss.Width(out) > width {
		out = ansi.Truncate(out, width, "") // a wide rune cut in half at the right edge
	}
	return out
}

// overlayDots draws one braille row of the pet at column x as a silhouette: from
// its first to its last dot the cells are the pet's (blank ones become spaces, so
// the text under the pet does not show through it), outside them the background stays.
func overlayDots(line string, x int, row string, width int) string {
	rs := []rune(row)
	blank := func(r rune) bool { return r == '\u2800' || r == ' ' }
	first, last := -1, -1
	for i, r := range rs {
		if !blank(r) {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		return line
	}
	seg := []rune{}
	for _, r := range rs[first : last+1] {
		if blank(r) {
			r = ' '
		}
		seg = append(seg, r)
	}
	return overlayAt(line, x+first, petStyle.Render(string(seg)), width)
}

// OverlayBottomRight draws fg over the bottom-right corner of bg (width × height),
// keeping the rest of bg as is.
func OverlayBottomRight(bg, fg string, width, height int) string {
	if fg == "" {
		return bg
	}
	bgLines := strings.Split(bg, "\n")
	for len(bgLines) < height {
		bgLines = append(bgLines, "")
	}
	fgLines := strings.Split(fg, "\n")
	fgW := 0
	for _, l := range fgLines {
		if w := lipgloss.Width(l); w > fgW {
			fgW = w
		}
	}
	left := width - fgW - 2
	if left < 0 || len(fgLines) > height {
		return bg
	}
	start := height - len(fgLines)
	for i, l := range fgLines {
		row := start + i
		if row < 0 || row >= len(bgLines) {
			continue
		}
		base := ansi.Truncate(bgLines[row], left, "")
		if w := lipgloss.Width(base); w < left {
			base += strings.Repeat(" ", left-w)
		}
		bgLines[row] = base + "\x1b[0m" + padRight(l, fgW)
	}
	return strings.Join(bgLines, "\n")
}

func padRight(s string, w int) string {
	if n := lipgloss.Width(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}
