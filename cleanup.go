package main

import (
	"claude-squad/session"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

// `cs cleanup` lists folders (площадки, worktree) that look abandoned, with their
// size, so the user can free disk and memory. It never deletes anything.

var (
	cleanupRoot string
	cleanupDays int
)

type folderInfo struct {
	Path     string
	Last     time.Time
	SizeMB   int64 // -1 when du did not finish
	Session  string
	Branch   string
	Empty    bool
	Worktree bool
}

// lastChange is the newest modification time among the folder and its first two
// levels (skipping node_modules and .git internals), cheap enough for big repos.
func lastChange(dir string) time.Time {
	var newest time.Time
	base := strings.Count(dir, string(os.PathSeparator))
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == "node_modules" || (d.Name() == ".git" && p != dir)) {
			return filepath.SkipDir
		}
		if info, err := d.Info(); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		if d.IsDir() && strings.Count(p, string(os.PathSeparator))-base >= 2 {
			return filepath.SkipDir
		}
		return nil
	})
	return newest
}

func duMB(dir string) int64 {
	cmd := exec.Command("du", "-sk", dir)
	done := make(chan []byte, 1)
	go func() { out, _ := cmd.Output(); done <- out }()
	select {
	case out := <-done:
		if f := strings.Fields(string(out)); len(f) > 0 {
			if kb, err := strconv.ParseInt(f[0], 10, 64); err == nil {
				return kb / 1024
			}
		}
	case <-time.After(90 * time.Second):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
	return -1
}

func isEmptyDir(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.Name() != ".DS_Store" {
			return false
		}
	}
	return true
}

// candidateFolders: top-level folders of root and the worktrees nested in them.
func candidateFolders(root string) []string {
	var out []string
	add := func(p string) {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			out = append(out, p)
		}
	}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		top := filepath.Join(root, e.Name())
		add(top)
		for _, sub := range []string{".worktrees", filepath.Join(".claude", "worktrees")} {
			nested, _ := os.ReadDir(filepath.Join(top, sub))
			for _, n := range nested {
				if n.IsDir() {
					add(filepath.Join(top, sub, n.Name()))
				}
			}
		}
	}
	return out
}

func scanFolders(root string, sessions map[string]string) []folderInfo {
	paths := candidateFolders(root)
	infos := make([]folderInfo, len(paths))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for k, p := range paths {
		wg.Add(1)
		go func(k int, p string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			fi := folderInfo{Path: p, Session: sessions[p], Empty: isEmptyDir(p)}
			if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
				fi.Worktree = true
				if out, err := exec.Command("git", "-C", p, "branch", "--show-current").Output(); err == nil {
					fi.Branch = strings.TrimSpace(string(out))
				}
			}
			fi.Last = lastChange(p)
			infos[k] = fi
		}(k, p)
	}
	wg.Wait()
	return infos
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Показать заброшенные площадки и worktree с размером (ничего не удаляет)",
	RunE: func(cmd *cobra.Command, args []string) error {
		root := cleanupRoot
		if root == "" {
			home, _ := os.UserHomeDir()
			root = filepath.Join(home, "devRepo")
		}
		sessions := map[string]string{}
		if snap, err := session.ReadStatusSnapshot(); err == nil {
			for _, s := range snap.Sessions {
				sessions[s.Path] = fmt.Sprintf("#%d %s", s.Number, s.Title)
			}
		}
		fmt.Printf("Смотрю %s (это займёт минуту — считаю размеры)…\n", root)
		infos := scanFolders(root, sessions)
		cutoff := time.Now().AddDate(0, 0, -cleanupDays)
		var stale, empty []folderInfo
		for _, fi := range infos {
			switch {
			case fi.Session != "":
				// in use
			case fi.Empty:
				empty = append(empty, fi)
			case fi.Last.Before(cutoff):
				stale = append(stale, fi)
			}
		}
		var wg sync.WaitGroup
		for k := range stale {
			wg.Add(1)
			go func(k int) { defer wg.Done(); stale[k].SizeMB = duMB(stale[k].Path) }(k)
		}
		wg.Wait()
		sort.Slice(stale, func(a, b int) bool { return stale[a].SizeMB > stale[b].SizeMB })

		var total int64
		fmt.Printf("\nНе трогались дольше %d дней и без сессии в Squad:\n", cleanupDays)
		if len(stale) == 0 {
			fmt.Println("  нет")
		}
		for _, fi := range stale {
			size := "  >?? ГБ (du не успел)"
			if fi.SizeMB >= 0 {
				size = fmt.Sprintf("%7.1f ГБ", float64(fi.SizeMB)/1024)
				if fi.SizeMB < 1024 {
					size = fmt.Sprintf("%7d МБ", fi.SizeMB)
				}
				total += fi.SizeMB
			}
			branch := ""
			if fi.Branch != "" {
				branch = "  [" + fi.Branch + "]"
			}
			fmt.Printf("  %s  %s  %s%s\n", size, fi.Last.Format("02.01"), fi.Path, branch)
		}
		if len(empty) > 0 {
			fmt.Printf("\nПустые папки (остатки удалённых площадок): %d\n", len(empty))
			for _, fi := range empty {
				fmt.Println("  " + fi.Path)
			}
		}
		fmt.Printf("\nМожно освободить примерно %.1f ГБ. Я ничего не удалял: worktree убирай через\n"+
			"`git worktree remove <путь>` (сначала убедись, что ветка запушена), пустые папки — `rmdir`.\n", float64(total)/1024)
		return nil
	},
}

func init() {
	cleanupCmd.Flags().StringVar(&cleanupRoot, "root", "", "где искать (по умолчанию ~/devRepo)")
	cleanupCmd.Flags().IntVar(&cleanupDays, "days", 14, "сколько дней без изменений считать заброшенным")
	cleanupCmd.SilenceUsage = true
	rootCmd.AddCommand(cleanupCmd)
}
