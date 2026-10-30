package ui

import (
	"claude-squad/session"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/require"
)

func TestPetMood(t *testing.T) {
	cases := map[float64]int{-1: 0, 0: 0, 0.9: 0, 1: 1, 3: 3, 5.9: 5, 6: 6, 7.9: 6, 8: 7, 10.4: 8, 13: 9, 17: 10, 21: 11, 24: 11}
	for d, want := range cases {
		require.Equal(t, want, PetMood(d), d)
	}
}

func TestPetRendersInBottomRightCorner(t *testing.T) {
	pet := RenderPet(PetConfig{Density: 10.4, HasValue: true}, time.Unix(0, 0))
	require.Contains(t, pet, "плотность 10,4 ч")
	require.Contains(t, pet, "Тян: «Обожаю, когда ты коммитишь!»")
	require.Contains(t, RenderPet(PetConfig{Density: 5, HasValue: true, Big: true}, time.Unix(0, 0)), "плотность 5 ч · почти норм")
	require.Len(t, tyanFrames, len(PetMoodLabels))
	require.Contains(t, RenderPet(PetConfig{Density: 10.4, HasValue: true, T39: 9.2, HasT39: true}, time.Unix(0, 0)), "плотность 10,4 ч · t39 9,2")
	bg := strings.Repeat(strings.Repeat("x", 100)+"\n", 29) + strings.Repeat("x", 100)
	out := OverlayBottomRight(bg, pet, 100, 30)
	lines := strings.Split(out, "\n")
	require.Len(t, lines, 30)
	require.Equal(t, strings.Repeat("x", 100), lines[0], "the top is untouched")
	for _, l := range lines {
		require.LessOrEqual(t, lipgloss.Width(l), 100)
	}
	require.Contains(t, lines[29], "плотность")
	require.True(t, strings.HasPrefix(lines[29], "xxxx"), "the left part of the line stays")
	require.Equal(t, "", RenderPet(PetConfig{Kind: "off"}, time.Now()))
	for m := range tyanFrames {
		for _, f := range tyanFrames[m] {
			require.Len(t, f, 16)
		}
	}
}

func TestPetGoalAndEmptyHour(t *testing.T) {
	pet := RenderPet(PetConfig{Density: 4, HasValue: true, Goal: 6, HourEmpty: true}, time.Unix(0, 0))
	require.Contains(t, pet, "до цели 2 ч")
	require.Contains(t, pet, "час пустой")
	pet = RenderPet(PetConfig{Density: 7, HasValue: true, Goal: 6}, time.Unix(0, 0))
	require.Contains(t, pet, "цель взята ✓")
	require.NotContains(t, pet, "час пустой")
	require.NotContains(t, RenderPet(PetConfig{Density: 2, HasValue: true}, time.Unix(0, 0)), "до цели")
}

func TestOSPet(t *testing.T) {
	require.Len(t, osFrames, len(PetMoodLabels))
	for _, kind := range []string{"tyan", "os", "dog", "cat"} {
		require.Len(t, petPhrases[kind], len(PetMoodLabels), kind)
		require.Len(t, chibiFrames[kind], len(PetMoodLabels), kind)
		for m, frames := range chibiFrames[kind] {
			for _, f := range frames {
				require.LessOrEqual(t, len(f), 12, "%s mood %d: the small pet is at most 12 lines", kind, m)
			}
		}
	}
	at := time.Unix(0, 0)
	low := RenderPet(PetConfig{Kind: "os", Density: 0.5, HasValue: true}, at)
	require.Contains(t, low, "ОС: «Что там по плотности??»")
	require.Contains(t, low, "плотность 0,5 ч")
	require.NotContains(t, low, "рыдает")
	high := RenderPet(PetConfig{Kind: "os", Density: 22, HasValue: true}, at)
	require.Contains(t, high, "ОС: «Легенда.»")
	require.NotEqual(t, PetPhrase(1, time.Unix(0, 0)), PetPhrase(1, time.Unix(10, 0)))
}

func TestNextPet(t *testing.T) {
	require.Equal(t, "os", NextPet("", false))
	require.Equal(t, "dog", NextPet("os", false))
	require.Equal(t, "cat", NextPet("dog", false))
	require.Equal(t, "all", NextPet("cat", false))
	require.Equal(t, "off", NextPet("all", false))
	require.Equal(t, "tyan", NextPet("off", false))
	require.Equal(t, "custom", NextPet("all", true))
	require.Equal(t, "tyan", NextPet("weird", false))
}

func TestHourCommitMark(t *testing.T) {
	at := time.Unix(0, 0)
	yes := RenderPet(PetConfig{Density: 5, HasValue: true, Goal: 6, HourCommit: 1}, at)
	require.Contains(t, yes, "до цели 1 ч · этот час ")
	require.Contains(t, yes, "✓")
	no := RenderPet(PetConfig{Density: 5, HasValue: true, Goal: 6, HourCommit: -1}, at)
	require.Contains(t, no, "✗")
	unknown := RenderPet(PetConfig{Density: 5, HasValue: true, Goal: 6}, at)
	require.NotContains(t, unknown, "этот час")
}

func TestAnimalPets(t *testing.T) {
	require.Len(t, dogFrames, len(PetMoodLabels))
	require.Len(t, catFrames, len(PetMoodLabels))
	at := time.Unix(0, 0)
	require.Contains(t, RenderPet(PetConfig{Kind: "dog", Density: 6, HasValue: true, Big: true}, at), "плотность 6 ч · доволен")
	require.Contains(t, RenderPet(PetConfig{Kind: "dog", Density: 6, HasValue: true}, at), "Щенок: «Гав! Цель взята!»")
	require.Contains(t, RenderPet(PetConfig{Kind: "cat", Density: 0, HasValue: true}, at), "Котик: «Мяу. Ни одного коммита. Я ухожу»")
	require.NotEqual(t, RenderPet(PetConfig{Kind: "dog", Density: 6, HasValue: true}, at), RenderPet(PetConfig{Kind: "cat", Density: 6, HasValue: true}, at))
}

func TestOwnPetPhrases(t *testing.T) {
	path := t.TempDir() + "/pet-phrases.json"
	require.NoError(t, os.WriteFile(path, []byte(`{"cat": {"6": ["Свой текст"]}}`), 0600))
	own := LoadPetPhrases(path)
	at := time.Unix(0, 0)
	require.Contains(t, RenderPet(PetConfig{Kind: "cat", Density: 6, HasValue: true, Phrases: own}, at), "Котик: «Свой текст»")
	require.Contains(t, RenderPet(PetConfig{Kind: "cat", Density: 0, HasValue: true, Phrases: own}, at), "Я ухожу", "moods left out keep the built-in lines")
	require.Nil(t, LoadPetPhrases(t.TempDir()+"/missing.json"))
}

func TestOverlayPetIsSeeThroughAndWalks(t *testing.T) {
	bg := strings.Repeat(strings.Repeat("x", 120)+"\n", 39) + strings.Repeat("x", 120)
	cfg := PetConfig{Kind: "cat", Density: 6, HasValue: true, Goal: 6}
	a := OverlayPet(bg, cfg, false, 120, 40, time.Unix(0, 0))
	lines := strings.Split(a, "\n")
	require.Len(t, lines, 40)
	for _, l := range lines {
		require.LessOrEqual(t, lipgloss.Width(l), 120)
	}
	require.Contains(t, lines[39], "цель взята")
	require.NotEqual(t, "", strings.Trim(session.StripANSI(lines[39]), "x"), "the pet stands on the bottom line")
	require.Contains(t, a, "Котик: «")
	// See-through: the background stays between the dots of the sprite.
	sprite := strings.Join(lines[39-13:39], "\n")
	require.Contains(t, sprite, "x", "background shows around the pet")
	require.NotContains(t, a, "⠀⠀⠀⠀", "blank braille is not drawn")
	// It walks: different places at different times, and mirrors when going left.
	seen := map[int]bool{}
	for s := int64(0); s < 120; s += 3 {
		x, _, _ := petWalk(time.Unix(s, 0), 100)
		seen[x] = true
		require.GreaterOrEqual(t, x, 0)
		require.LessOrEqual(t, x, 100)
	}
	require.Greater(t, len(seen), 5)
}

func TestMirrorSprite(t *testing.T) {
	require.Equal(t, []string{"⠈⠁"}, mirrorSprite([]string{"⠈⠁"})) // ⠁ dot1 ↔ ⠈ dot4, order reversed
	require.Equal(t, []string{"⢀"}, mirrorSprite([]string{"⡀"}))   // dot7 ↔ dot8
}

func TestAllPetsTogether(t *testing.T) {
	bg := strings.Repeat(strings.Repeat("x", 160)+"\n", 39) + strings.Repeat("x", 160)
	out := OverlayPet(bg, PetConfig{Kind: "all", Density: 6, HasValue: true}, false, 160, 40, time.Unix(0, 0))
	lines := strings.Split(out, "\n")
	require.Len(t, lines, 40)
	speeches := 0
	for _, who := range []string{"Тян: «", "ОС: «", "Щенок: «", "Котик: «"} {
		if strings.Contains(out, who) {
			speeches++
		}
	}
	require.Equal(t, 1, speeches, "one pet speaks at a time")
	for _, l := range lines {
		require.LessOrEqual(t, lipgloss.Width(l), 160)
	}
}

func TestAllPetsDoNotOverlap(t *testing.T) {
	// For many moments in time, the four pets' columns never intersect.
	spriteW := len([]rune(padSprite(chibiFrames["tyan"][6][0])[0]))
	width := 160
	walkMax := width - 30 - spriteW - 3
	lane := (walkMax + spriteW) / 4
	for sec := int64(0); sec < 300; sec += 7 {
		var xs []int
		for n := 0; n < 4; n++ {
			dx, _, _ := petWalk(time.Unix(sec, 0).Add(time.Duration(n)*7300*time.Millisecond), lane-spriteW-1)
			xs = append(xs, n*lane+dx)
		}
		for a := 1; a < 4; a++ {
			require.GreaterOrEqual(t, xs[a]-xs[a-1], spriteW, "pets %d and %d overlap at %ds", a-1, a, sec)
		}
	}
}

func TestWebPetRowsAligned(t *testing.T) {
	info := PetsForWeb(PetConfig{Kind: "all", HasValue: true, Density: 6}, time.Now())
	for _, p := range info.Pets {
		for _, set := range [][][]string{p.Stand, p.StandL, p.Walk, p.WalkL} {
			for _, f := range set {
				w := len([]rune(f[0]))
				for _, r := range f {
					if len([]rune(r)) != w {
						t.Fatalf("%s: rows of different width (%d vs %d)", p.Kind, len([]rune(r)), w)
					}
				}
			}
		}
	}
}
