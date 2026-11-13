package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"claude-squad/log"
	"claude-squad/session"
)

// vpnFixer reopens the VPN client when the VPN stays down — the client sometimes hangs
// on «Reconnecting…» — and tells the user once if that did not help (e.g. it wants a
// fresh login, which Squad can not and should not do).
type vpnFixer struct {
	restarts int
	lastAt   time.Time
	warned   bool
}

const (
	vpnFixFirst = 7 * time.Minute // Ideco itself retries by token for 420 s; reopening earlier breaks that
	vpnFixEvery = 15 * time.Minute
	vpnFixMax   = 3
	vpnFixWarn  = 2 * time.Minute // after a reopen, how long to wait before telling the user
)

// restartVPNApp quits and reopens the VPN client app; a variable for tests.
var restartVPNApp = restartVPNAppOS

func (f *vpnFixer) tick(app string, downSince, now time.Time) {
	if app == "" {
		return
	}
	if f.restarts > 0 && !f.warned && now.Sub(f.lastAt) >= vpnFixWarn {
		f.warned = true
		notifyUser("orchestraByVitalik: VPN не поднялся",
			app+" перезапущен, но подключения нет. Скорее всего, нужно войти заново.")
	}
	due := downSince.Add(vpnFixFirst)
	if f.restarts > 0 {
		due = f.lastAt.Add(vpnFixEvery)
	}
	if f.restarts >= vpnFixMax || now.Before(due) {
		return
	}
	f.restarts++
	f.lastAt = now
	log.InfoLog.Printf("VPN down since %s — reopening %s (%d/%d)", downSince.Format("15:04:05"), app, f.restarts, vpnFixMax)
	session.AppendEvent(session.Event{At: now, Kind: "status", Text: fmt.Sprintf("VPN лежит — перезапускаю %s (%d/%d)", app, f.restarts, vpnFixMax)})
	restartVPNApp(app)
}

func restartVPNAppOS(app string) {
	if runtime.GOOS != "darwin" {
		return
	}
	go func() {
		quit := fmt.Sprintf(`tell application "%s" to quit`, app)
		_ = exec.Command("osascript", "-e", quit).Run()
		time.Sleep(3 * time.Second)
		_ = exec.Command("pkill", "-x", app).Run() // if it ignored the quit
		time.Sleep(time.Second)
		if err := exec.Command("open", "-a", app).Run(); err != nil {
			log.WarningLog.Printf("reopen %s: %v", app, err)
		}
	}()
}

// captureVPNDrop saves what the network looked like right after the VPN dropped, for
// the admin: DNS, routes, interfaces and the VPN client's own fresh logs. It keeps the
// last ten drops in ~/.claude-squad/vpn-drops.
var captureVPNDrop = captureVPNDropOS

const vpnCaptureScript = `
exec 2>&1
echo "== $(date)"; echo
echo "== scutil --dns"; scutil --dns | head -60; echo
echo "== default routes"; netstat -rn -f inet | grep -E '^(default|0/1|128.0/1)' ; echo
echo "== route to the VPN check hosts"; for h in $HOSTS; do echo "-- $h"; route -n get "${h%%:*}" 2>&1 | grep -E 'interface|gateway'; done; echo
echo "== interfaces"; ifconfig | grep -E '^[a-z]|inet ' ; echo
echo "== wifi"; networksetup -getairportnetwork en0 2>&1; echo
echo "== processes"; pgrep -lf -i 'ideco|tailscale|proxifier' | cut -c1-200; echo
mkdir -p logs
find "$HOME/Library/Logs" "$HOME/Library/Application Support" "/Library/Logs" "/Library/Application Support" /var/log -maxdepth 3 -iname '*ideco*' 2>/dev/null | while read -r p; do
  find "$p" -maxdepth 2 -type f \( -name '*.log' -o -name '*.diag' -o -name '*.txt' \) -mmin -60 2>/dev/null | head -20 | while read -r f; do
    tail -c 2000000 "$f" > "logs/$(echo "$f" | tr '/ ' '__')" 2>/dev/null
  done
done
echo "== copied logs"; ls -la logs
`

func captureVPNDropOS(at time.Time, hosts []string) {
	if runtime.GOOS != "darwin" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	root := filepath.Join(home, ".claude-squad", "vpn-drops")
	dir := filepath.Join(root, at.Format("2006-01-02T15-04-05"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	cmd := exec.Command("bash", "-c", vpnCaptureScript)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HOSTS="+strings.Join(hosts, " "))
	out, _ := cmd.CombinedOutput()
	_ = os.WriteFile(filepath.Join(dir, "network.txt"), out, 0o644)
	log.InfoLog.Printf("VPN drop captured to %s", dir)
	pruneDirs(root, 10)
}

// pruneDirs keeps the newest n entries of root (names sort by time).
func pruneDirs(root string, n int) {
	es, err := os.ReadDir(root)
	if err != nil || len(es) <= n {
		return
	}
	names := make([]string, 0, len(es))
	for _, e := range es {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names[:len(names)-n] {
		_ = os.RemoveAll(filepath.Join(root, name))
	}
}
