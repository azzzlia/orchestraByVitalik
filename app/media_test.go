package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMediaPaths(t *testing.T) {
	got := mediaPaths("Скрин: .ai/qa-screens/782059/01-ok-звук.png, видео /Users/v/Desktop/rec.mov и https://x.ru/a.png. Read(/tmp/a.PNG)")
	want := []string{".ai/qa-screens/782059/01-ok-звук.png", "/Users/v/Desktop/rec.mov", "/tmp/a.PNG"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("%q", got)
	}
	if mediaPaths("report.html и main.go") != nil {
		t.Fatal("не картинки")
	}
	h := mediaHTML([]string{"a.png", "b.mov"})
	if !strings.Contains(h, `class="c-img"`) || !strings.Contains(h, "<video") {
		t.Fatal(h)
	}
}

func TestResolveSessionFile(t *testing.T) {
	root := t.TempDir()
	out := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "shots"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "shots", "a.png"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(out, "secret.png"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644)
	_ = os.Symlink(filepath.Join(out, "secret.png"), filepath.Join(root, "link.png"))
	if resolveSessionFile(root, "shots/a.png") == "" || resolveSessionFile(root, filepath.Join(root, "shots", "a.png")) == "" {
		t.Fatal("файл сессии не отдан")
	}
	for _, p := range []string{filepath.Join(out, "secret.png"), "../" + filepath.Base(out) + "/secret.png", "link.png", "notes.txt", "shots/none.png"} {
		if resolveSessionFile(root, p) != "" {
			t.Fatalf("отдали лишнее: %s", p)
		}
	}
}
