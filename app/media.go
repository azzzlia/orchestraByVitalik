package app

import (
	"html"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Pictures and videos in the chat: paths the user's message, the agent's answer or a
// tool call names (a screenshot the agent took, a file the user attached) are shown
// as a preview. The page loads them from /api/file, which serves only files inside the
// session's own folder.

var mediaRe = regexp.MustCompile(`(?i)(?:^|[\s"'` + "`" + `(\[=:,])((?:~/|/|\./)?[\p{L}\p{N}_./~+@-]+\.(?:png|jpe?g|gif|webp|mov|mp4|webm|m4v))(?:$|[\s"'` + "`" + `)\],.:;!?])`)

var mediaTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp",
	".mov": "video/quicktime", ".mp4": "video/mp4", ".webm": "video/webm", ".m4v": "video/mp4",
}

func isVideo(p string) bool {
	return strings.HasPrefix(mediaTypes[strings.ToLower(filepath.Ext(p))], "video/")
}

// mediaPaths: the picture and video paths named in a text, in order, without repeats.
func mediaPaths(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range mediaRe.FindAllStringSubmatch(text, -1) {
		p := strings.TrimRight(m[1], ".")
		if p == "" || seen[p] || strings.Contains(p, "://") || strings.HasPrefix(p, "//") {
			continue
		}
		seen[p] = true
		out = append(out, p)
		if len(out) == 6 {
			break
		}
	}
	return out
}

// mediaHTML: previews for the paths; the page fills in each src (it knows the session).
func mediaHTML(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<div class="c-media">`)
	for _, p := range paths {
		q := html.EscapeString(url.QueryEscape(p))
		name := html.EscapeString(filepath.Base(p))
		if isVideo(p) {
			b.WriteString(`<figure class="c-vid"><video controls preload="metadata" playsinline data-p="` + q + `"></video><figcaption>` + name + `</figcaption></figure>`)
		} else {
			b.WriteString(`<a class="c-img" target="_blank" rel="noopener" data-p="` + q + `" title="` + name + `"><img loading="lazy" alt="` + name + `"></a>`)
		}
	}
	b.WriteString(`</div>`)
	return b.String()
}

// resolveSessionFile: the real path of p if it lies inside root (relative paths count
// from root, ~/ is the home folder), else "".
func resolveSessionFile(root, p string) string {
	if root == "" || p == "" || mediaTypes[strings.ToLower(filepath.Ext(p))] == "" {
		return ""
	}
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		p = filepath.Join(home, p[2:])
	} else if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return ""
	}
	rroot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(rroot, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return ""
	}
	return real
}

// webFile serves a picture or a video from the session's folder (n, p).
func (m *home) webFile(w http.ResponseWriter, r *http.Request) {
	n := formInt(r, "n")
	m.mu.Lock()
	inst := m.findByNumber(n)
	root := ""
	if inst != nil {
		root = inst.Path
	}
	m.mu.Unlock()
	real := resolveSessionFile(root, r.FormValue("p"))
	if real == "" {
		http.Error(w, "нет такого файла в папке сессии", http.StatusNotFound)
		return
	}
	f, err := os.Open(real)
	if err != nil {
		http.Error(w, "не открылся", http.StatusNotFound)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.Error(w, "не файл", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", mediaTypes[strings.ToLower(filepath.Ext(real))])
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, filepath.Base(real), st.ModTime(), f)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
