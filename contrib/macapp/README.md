# Приложение «Оркестр» для Mac

Окно с веб-интерфейсом Squad (WebKit). Сам Squad работает в фоне — в tmux-сессии `orchestra`
(`~/bin/orchestra-start`), поэтому терминал можно закрывать.

Собирает его обёртка `sq` при обновлении: `swiftc -O main.swift -framework Cocoa -framework WebKit`
→ `~/Applications/Orchestra.app` (подпись ad-hoc). Нужны Command Line Tools (`xcode-select --install`).

- `main.swift` — приложение; `Info.plist`; `AppIcon.icns` (из `icon.py`).
- Меню: «Squad в терминале» (⌘T, `tmux attach -t orchestra`), «Перезапустить Squad»,
  «Запускать Squad при входе в систему» (LaunchAgent `com.vitalik.orchestra`), ⌘R, ⌘+/⌘−.
- ⌘Q закрывает только окно-приложение: Squad продолжает работать.
