package app

import (
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"
)

// ansiToHTML turns a captured tmux screen (with SGR colors) into HTML spans for the
// web terminal, and marks the cursor cell. Only colors and bold/dim/italic/underline/
// reverse are kept; other escape sequences are dropped.

type sgr struct {
	fg, bg                            string
	bold, dim, italic, under, reverse bool
}

var ansi16 = []string{"#15161e", "#f7768e", "#9ece6a", "#e0af68", "#7aa2f7", "#bb9af7", "#7dcfff", "#a9b1d6",
	"#414868", "#ff899d", "#9fe044", "#faba4a", "#8db0ff", "#c7a9ff", "#a4daff", "#c0caf5"}

func xterm256(n int) string {
	switch {
	case n < 16:
		return ansi16[n]
	case n < 232:
		n -= 16
		lv := []int{0, 95, 135, 175, 215, 255}
		return fmt.Sprintf("#%02x%02x%02x", lv[n/36], lv[(n/6)%6], lv[n%6])
	case n < 256:
		g := 8 + (n-232)*10
		return fmt.Sprintf("#%02x%02x%02x", g, g, g)
	}
	return ""
}

func (s *sgr) apply(params string) {
	if params == "" {
		params = "0"
	}
	p := strings.Split(strings.ReplaceAll(params, ":", ";"), ";")
	for i := 0; i < len(p); i++ {
		n, _ := strconv.Atoi(p[i])
		switch {
		case n == 0:
			*s = sgr{}
		case n == 1:
			s.bold = true
		case n == 2:
			s.dim = true
		case n == 3:
			s.italic = true
		case n == 4:
			s.under = true
		case n == 7:
			s.reverse = true
		case n == 22:
			s.bold, s.dim = false, false
		case n == 23:
			s.italic = false
		case n == 24:
			s.under = false
		case n == 27:
			s.reverse = false
		case n >= 30 && n <= 37:
			s.fg = ansi16[n-30]
		case n == 39:
			s.fg = ""
		case n >= 40 && n <= 47:
			s.bg = ansi16[n-40]
		case n == 49:
			s.bg = ""
		case n >= 90 && n <= 97:
			s.fg = ansi16[n-90+8]
		case n >= 100 && n <= 107:
			s.bg = ansi16[n-100+8]
		case n == 38 || n == 48:
			col := ""
			if i+2 < len(p) && p[i+1] == "5" {
				c, _ := strconv.Atoi(p[i+2])
				col = xterm256(c)
				i += 2
			} else if i+4 < len(p) && p[i+1] == "2" {
				r, _ := strconv.Atoi(p[i+2])
				g, _ := strconv.Atoi(p[i+3])
				b, _ := strconv.Atoi(p[i+4])
				col = fmt.Sprintf("#%02x%02x%02x", r&255, g&255, b&255)
				i += 4
			}
			if n == 38 {
				s.fg = col
			} else {
				s.bg = col
			}
		}
	}
}

func (s sgr) style() string {
	fg, bg := s.fg, s.bg
	if s.reverse {
		if fg == "" {
			fg = "var(--tfg)"
		}
		if bg == "" {
			bg = "var(--tbg)"
		}
		fg, bg = bg, fg
	}
	var b strings.Builder
	if fg != "" {
		b.WriteString("color:" + fg + ";")
	}
	if bg != "" {
		b.WriteString("background:" + bg + ";")
	}
	if s.bold {
		b.WriteString("font-weight:700;")
	}
	if s.dim {
		b.WriteString("opacity:.6;")
	}
	if s.italic {
		b.WriteString("font-style:italic;")
	}
	if s.under {
		b.WriteString("text-decoration:underline;")
	}
	return b.String()
}

// ansiToHTML renders lines; cx, cy is the cursor cell (-1 to hide it).
func ansiToHTML(lines []string, cx, cy int) string {
	var out strings.Builder
	for row, line := range lines {
		var st sgr
		open := ""
		col := 0
		cursorDone := row != cy || cx < 0
		var run strings.Builder
		flush := func() {
			if run.Len() == 0 {
				return
			}
			if open != "" {
				out.WriteString(`<span style="` + open + `">` + html.EscapeString(run.String()) + `</span>`)
			} else {
				out.WriteString(html.EscapeString(run.String()))
			}
			run.Reset()
		}
		rs := []rune(line)
		for i := 0; i < len(rs); i++ {
			r := rs[i]
			if r == 0x1b && i+1 < len(rs) {
				switch rs[i+1] {
				case '[': // CSI: parameters, then a final byte
					j := i + 2
					for j < len(rs) && (rs[j] < 0x40 || rs[j] > 0x7e) {
						j++
					}
					if j < len(rs) && rs[j] == 'm' {
						flush()
						st.apply(string(rs[i+2 : j]))
						open = st.style()
					}
					i = j
					continue
				case ']': // OSC (links, titles) up to BEL or ST
					j := i + 2
					for j < len(rs) && rs[j] != 0x07 && !(rs[j] == 0x1b && j+1 < len(rs) && rs[j+1] == '\\') {
						j++
					}
					if j < len(rs) && rs[j] == 0x1b {
						j++
					}
					i = j
					continue
				default:
					i++
					continue
				}
			}
			if r < 0x20 {
				continue
			}
			if !cursorDone && col == cx {
				flush()
				cs := open + "background:var(--cur);color:var(--tbg);"
				out.WriteString(`<span class="cur" style="` + cs + `">` + html.EscapeString(string(r)) + `</span>`)
				cursorDone = true
				col += runewidth.RuneWidth(r)
				continue
			}
			run.WriteRune(r)
			col += runewidth.RuneWidth(r)
		}
		flush()
		if !cursorDone && cx >= col {
			out.WriteString(strings.Repeat(" ", cx-col))
			out.WriteString(`<span class="cur" style="background:var(--cur)"> </span>`)
		}
		if row < len(lines)-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}
