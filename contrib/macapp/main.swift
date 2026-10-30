// Оркестр — macOS app for orchestraByVitalik: a native window with Squad's web
// interface. Squad itself runs in the background (tmux session "orchestra"), so the
// terminal can be closed; quitting the app leaves Squad running.
//
// Built by the sq wrapper on update: swiftc -O main.swift -framework Cocoa -framework WebKit

import Cocoa
import WebKit

let home = FileManager.default.homeDirectoryForCurrentUser
let squadDir = home.appendingPathComponent(".claude-squad")

/// The web interface port: "web_port" from config.json, 8787 by default.
func webPort() -> Int {
    if let data = try? Data(contentsOf: squadDir.appendingPathComponent("config.json")),
       let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
       let p = obj["web_port"] as? Int, p > 0, p < 65536 {
        return p
    }
    return 8787
}

/// The key that opens the web interface (Squad writes it on start).
func webKey() -> String? {
    guard let s = try? String(contentsOf: squadDir.appendingPathComponent("web-token"), encoding: .utf8) else { return nil }
    let k = s.trimmingCharacters(in: .whitespacesAndNewlines)
    return k.count >= 32 ? k : nil
}

/// Runs a shell command in a login shell (so PATH has tmux, claude, ~/bin).
@discardableResult
func shell(_ cmd: String) -> Int32 {
    let p = Process()
    p.executableURL = URL(fileURLWithPath: "/bin/zsh")
    p.arguments = ["-lc", cmd]
    p.standardOutput = FileHandle.nullDevice
    p.standardError = FileHandle.nullDevice
    do { try p.run() } catch { return -1 }
    p.waitUntilExit()
    return p.terminationStatus
}

let launchAgent = home.appendingPathComponent("Library/LaunchAgents/com.vitalik.orchestra.plist")

final class AppDelegate: NSObject, NSApplicationDelegate, WKNavigationDelegate, WKUIDelegate {
    var window: NSWindow!
    var web: WKWebView!
    var tries = 0
    var connected = false
    var loginItem: NSMenuItem!

    func applicationDidFinishLaunching(_ notification: Notification) {
        buildMenu()
        let cfg = WKWebViewConfiguration()
        cfg.websiteDataStore = .default()
        web = WKWebView(frame: .zero, configuration: cfg)
        web.navigationDelegate = self
        web.uiDelegate = self
        web.allowsMagnification = true
        web.setValue(false, forKey: "drawsBackground") // no white flash before the page

        window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 1440, height: 900),
                          styleMask: [.titled, .closable, .miniaturizable, .resizable],
                          backing: .buffered, defer: false)
        window.title = "orchestraByVitalik"
        window.titlebarAppearsTransparent = true
        window.backgroundColor = NSColor(red: 0.067, green: 0.059, blue: 0.09, alpha: 1)
        window.contentView = web
        window.minSize = NSSize(width: 720, height: 480)
        window.center()
        window.setFrameAutosaveName("OrchestraMain")
        window.isReleasedWhenClosed = false
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
        connect()
    }

    // Closing the window keeps the app (and Squad) running; the Dock icon brings it back.
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }

    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        window.makeKeyAndOrderFront(nil)
        return true
    }

    // MARK: connecting to Squad

    func connect() {
        let port = webPort()
        guard let url = URL(string: "http://127.0.0.1:\(port)/api/state") else { return }
        var req = URLRequest(url: url)
        req.timeoutInterval = 2
        URLSession.shared.dataTask(with: req) { _, resp, _ in
            DispatchQueue.main.async {
                if resp != nil, let key = webKey(),
                   let page = URL(string: "http://127.0.0.1:\(port)/?key=\(key)&app=1") {
                    self.connected = true
                    self.tries = 0
                    self.web.load(URLRequest(url: page))
                } else {
                    self.waitForSquad()
                }
            }
        }.resume()
    }

    func waitForSquad() {
        connected = false
        if tries == 0 {
            showMessage("Запускаю Squad…", "Он будет работать в фоне — терминал можно закрыть.")
            DispatchQueue.global().async { shell("\"$HOME/bin/orchestra-start\"") }
        }
        tries += 1
        if tries > 45 {
            showMessage("Squad не запустился",
                        "Запусти его в терминале командой <code>sq</code> или <code>orchestra-start</code> и нажми ⌘R.")
            tries = 0
            return
        }
        DispatchQueue.main.asyncAfter(deadline: .now() + 2) { self.connect() }
    }

    func showMessage(_ title: String, _ text: String) {
        let html = """
        <html><body style="margin:0;height:100vh;display:flex;align-items:center;justify-content:center;
        background:#110f17;color:#f1eff7;font:15px -apple-system,system-ui">
        <div style="text-align:center;max-width:460px;line-height:1.5"><div style="font-size:22px;font-weight:700;margin-bottom:8px">
        orchestra<span style="color:#ec6f9c">By</span>Vitalik</div><div style="font-size:17px;margin-bottom:6px">\(title)</div>
        <div style="color:#9a96ad">\(text)</div></div></body></html>
        """
        web.loadHTMLString(html, baseURL: nil)
    }

    // The page went away (Squad restarted for an update): reconnect.
    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
        if connected { tries = 1; DispatchQueue.main.asyncAfter(deadline: .now() + 2) { self.connect() } }
    }

    func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) {
        if connected { tries = 1; DispatchQueue.main.asyncAfter(deadline: .now() + 2) { self.connect() } }
    }

    // Links to other sites open in the browser, not in the app window.
    func webView(_ webView: WKWebView, decidePolicyFor navigationAction: WKNavigationAction,
                 decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
        if let url = navigationAction.request.url, let host = url.host,
           host != "127.0.0.1", host != "localhost", url.scheme == "http" || url.scheme == "https" {
            NSWorkspace.shared.open(url)
            decisionHandler(.cancel)
            return
        }
        decisionHandler(.allow)
    }

    func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration,
                 for navigationAction: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? {
        if let url = navigationAction.request.url { NSWorkspace.shared.open(url) }
        return nil
    }

    // alert / confirm / prompt from the page (WebKit shows nothing without these).
    func webView(_ webView: WKWebView, runJavaScriptAlertPanelWithMessage message: String,
                 initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping () -> Void) {
        let a = NSAlert()
        a.messageText = message
        a.addButton(withTitle: "OK")
        a.beginSheetModal(for: window) { _ in completionHandler() }
    }

    func webView(_ webView: WKWebView, runJavaScriptConfirmPanelWithMessage message: String,
                 initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping (Bool) -> Void) {
        let a = NSAlert()
        a.messageText = message
        a.addButton(withTitle: "Да")
        a.addButton(withTitle: "Отмена")
        a.beginSheetModal(for: window) { r in completionHandler(r == .alertFirstButtonReturn) }
    }

    func webView(_ webView: WKWebView, runJavaScriptTextInputPanelWithPrompt prompt: String, defaultText: String?,
                 initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping (String?) -> Void) {
        let a = NSAlert()
        a.messageText = prompt
        let field = NSTextField(frame: NSRect(x: 0, y: 0, width: 280, height: 24))
        field.stringValue = defaultText ?? ""
        a.accessoryView = field
        a.addButton(withTitle: "OK")
        a.addButton(withTitle: "Отмена")
        a.window.initialFirstResponder = field
        a.beginSheetModal(for: window) { r in completionHandler(r == .alertFirstButtonReturn ? field.stringValue : nil) }
    }

    // MARK: menu

    func buildMenu() {
        let main = NSMenu()

        let appItem = NSMenuItem()
        main.addItem(appItem)
        let app = NSMenu()
        app.addItem(withTitle: "О программе «Оркестр»", action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)), keyEquivalent: "")
        app.addItem(.separator())
        app.addItem(withTitle: "Squad в терминале", action: #selector(openTerminal), keyEquivalent: "t")
        app.addItem(withTitle: "Перезапустить Squad", action: #selector(restartSquad), keyEquivalent: "")
        loginItem = app.addItem(withTitle: "Запускать Squad при входе в систему", action: #selector(toggleLogin), keyEquivalent: "")
        loginItem.state = FileManager.default.fileExists(atPath: launchAgent.path) ? .on : .off
        app.addItem(.separator())
        app.addItem(withTitle: "Скрыть", action: #selector(NSApplication.hide(_:)), keyEquivalent: "h")
        app.addItem(withTitle: "Выйти (Squad работает дальше)", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        appItem.submenu = app

        let editItem = NSMenuItem()
        main.addItem(editItem)
        let edit = NSMenu(title: "Правка")
        edit.addItem(withTitle: "Отменить", action: Selector(("undo:")), keyEquivalent: "z")
        edit.addItem(withTitle: "Повторить", action: Selector(("redo:")), keyEquivalent: "Z")
        edit.addItem(.separator())
        edit.addItem(withTitle: "Вырезать", action: #selector(NSText.cut(_:)), keyEquivalent: "x")
        edit.addItem(withTitle: "Копировать", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        edit.addItem(withTitle: "Вставить", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
        edit.addItem(withTitle: "Выбрать всё", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
        editItem.submenu = edit

        let viewItem = NSMenuItem()
        main.addItem(viewItem)
        let view = NSMenu(title: "Вид")
        view.addItem(withTitle: "Обновить", action: #selector(reload), keyEquivalent: "r")
        view.addItem(withTitle: "Крупнее", action: #selector(zoomIn), keyEquivalent: "=")
        view.addItem(withTitle: "Мельче", action: #selector(zoomOut), keyEquivalent: "-")
        view.addItem(withTitle: "Обычный размер", action: #selector(zoomReset), keyEquivalent: "0")
        view.addItem(.separator())
        let fs = view.addItem(withTitle: "Во весь экран", action: #selector(NSWindow.toggleFullScreen(_:)), keyEquivalent: "f")
        fs.keyEquivalentModifierMask = [.command, .control]
        viewItem.submenu = view

        let winItem = NSMenuItem()
        main.addItem(winItem)
        let win = NSMenu(title: "Окно")
        win.addItem(withTitle: "Свернуть", action: #selector(NSWindow.performMiniaturize(_:)), keyEquivalent: "m")
        win.addItem(withTitle: "Закрыть окно", action: #selector(NSWindow.performClose(_:)), keyEquivalent: "w")
        winItem.submenu = win
        NSApp.windowsMenu = win

        NSApp.mainMenu = main
    }

    @objc func reload() {
        if connected { web.reload() } else { tries = 0; connect() }
    }

    @objc func zoomIn() { web.pageZoom = min(2.0, web.pageZoom + 0.1) }
    @objc func zoomOut() { web.pageZoom = max(0.5, web.pageZoom - 0.1) }
    @objc func zoomReset() { web.pageZoom = 1.0 }

    @objc func openTerminal() {
        let script = """
        tell application "Terminal"
            activate
            do script "tmux set-option -w -t orchestra window-size latest 2>/dev/null; tmux attach -t orchestra || echo 'Squad работает не в фоне — он открыт в другом окне терминала'"
        end tell
        """
        var err: NSDictionary?
        NSAppleScript(source: script)?.executeAndReturnError(&err)
    }

    @objc func restartSquad() {
        let signal = squadDir.appendingPathComponent("update/restart")
        try? FileManager.default.createDirectory(at: signal.deletingLastPathComponent(), withIntermediateDirectories: true)
        FileManager.default.createFile(atPath: signal.path, contents: Data())
    }

    @objc func toggleLogin() {
        let fm = FileManager.default
        if fm.fileExists(atPath: launchAgent.path) {
            shell("launchctl unload '\(launchAgent.path)'")
            try? fm.removeItem(at: launchAgent)
            loginItem.state = .off
            return
        }
        let plist = """
        <?xml version="1.0" encoding="UTF-8"?>
        <!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
        <plist version="1.0"><dict>
          <key>Label</key><string>com.vitalik.orchestra</string>
          <key>ProgramArguments</key><array><string>/bin/zsh</string><string>-lc</string><string>"$HOME/bin/orchestra-start"</string></array>
          <key>RunAtLoad</key><true/>
        </dict></plist>
        """
        try? fm.createDirectory(at: launchAgent.deletingLastPathComponent(), withIntermediateDirectories: true)
        if (try? plist.write(to: launchAgent, atomically: true, encoding: .utf8)) != nil {
            loginItem.state = .on
        }
    }
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.setActivationPolicy(.regular)
app.run()
