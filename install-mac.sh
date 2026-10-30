#!/bin/bash
# Установка orchestraByVitalik на macOS. Запуск из папки репы:  ./install-mac.sh
# Повторный запуск — обновление: пересобирает и докладывает новое, твои правила
# дирижёра (rules.md и т.п.) и настройки не трогает.
set -uo pipefail

REPO="$(cd "$(dirname "$0")" && pwd)"
B=$'\033[1m'; G=$'\033[32m'; Y=$'\033[33m'; R=$'\033[31m'; N=$'\033[0m'
step() { printf '\n%s▸ %s%s\n' "$B" "$1" "$N"; }
ok()   { printf '  %s✓%s %s\n' "$G" "$N" "$1"; }
warn() { printf '  %s!%s %s\n' "$Y" "$N" "$1"; }
die()  { printf '\n%s✗ %s%s\n' "$R" "$1" "$N"; exit 1; }
ask()  { # ask "вопрос" "по умолчанию" → $REPLY
  local a; NOTTY=""; read -r -p "  $1 [${2}]: " a 2>/dev/null </dev/tty || { a=""; NOTTY=1; echo; }; REPLY="${a:-$2}"; }
agree() { # agree "вопрос" (по умолчанию да)
  local a; read -r -p "  $1 [Д/н]: " a 2>/dev/null </dev/tty || return 1; case "$a" in [nNнН]*) return 1;; *) return 0;; esac; }

printf '%sorchestraByVitalik%s — установка\n' "$B" "$N"

# ---------------------------------------------------------------- что нужно
step "Проверяю, что всё нужное есть"
[ "$(uname)" = Darwin ] || die "Нужна macOS."
if ! xcode-select -p >/dev/null 2>&1; then
  xcode-select --install >/dev/null 2>&1
  die "Нужны инструменты разработчика Apple: открылось окно установки. Поставь их и запусти ./install-mac.sh ещё раз."
fi
ok "инструменты разработчика Apple"
if ! command -v brew >/dev/null 2>&1; then
  for b in /opt/homebrew/bin/brew /usr/local/bin/brew; do [ -x "$b" ] && eval "$("$b" shellenv)"; done
fi
command -v brew >/dev/null 2>&1 || die "Нужен Homebrew: поставь по инструкции с https://brew.sh и запусти ./install-mac.sh ещё раз."
ok "Homebrew"
need=""
for t in tmux go git; do command -v "$t" >/dev/null 2>&1 || need="$need $t"; done
if [ -n "$need" ]; then
  echo "  ставлю:$need"
  brew install $need || die "brew install$need не вышел."
fi
ok "tmux, go, git"
command -v terminal-notifier >/dev/null 2>&1 || { brew install terminal-notifier >/dev/null 2>&1 && ok "terminal-notifier (уведомления)"; }
CLAUDE_BIN="$(command -v claude || true)"
[ -n "$CLAUDE_BIN" ] || die "Нужен Claude Code: поставь его (https://docs.claude.com/en/docs/claude-code), запусти claude и войди. Потом ./install-mac.sh ещё раз."
ok "Claude Code: $CLAUDE_BIN"

# ---------------------------------------------------------------- свои пути
step "Где ты работаешь"
mkdir -p "$HOME/.claude-squad"
ENVF="$HOME/.claude-squad/sq.env"
SQ_DIR=""
[ -f "$ENVF" ] && . "$ENVF"
def="${SQ_DIR:-}"
[ -z "$def" ] && [ -d "$HOME/devRepo/modules" ] && def="$HOME/devRepo/modules"
[ -z "$def" ] && def="$HOME"
echo "  Папка репозитория, в которой будут работать сессии Claude (например, modules)."
while :; do
  ask "Папка" "$def"; d="${REPLY/#\~/$HOME}"
  if [ ! -d "$d" ]; then warn "нет такой папки: $d"; [ -n "$NOTTY" ] && die "Запусти установку в терминале."; continue; fi
  git -C "$d" rev-parse --git-dir >/dev/null 2>&1 && break
  warn "это не git-репозиторий — Squad работает в папке репы (воркеры получают свои ветки)"
  [ -n "$NOTTY" ] && die "Запусти установку в терминале."
done
SQ_DIR="$(cd "$d" && pwd)"
SQ_REPOS="$(dirname "$SQ_DIR")"

# Профиль команды: общий (по умолчанию) или готовые настройки команды из contrib/profiles.
profiles=()
for pj in "$REPO"/contrib/profiles/*/profile.json; do
  n="$(basename "$(dirname "$pj")")"; [ -f "$pj" ] && [ "$n" != example ] && profiles+=("$n")
done
title() { /usr/bin/python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); print(d["title"]+" — "+d.get("about",""))' "$REPO/contrib/profiles/$1/profile.json" 2>/dev/null || echo "$1"; }
PROFILE="${PROFILE:-}"
if [ "${#profiles[@]}" -gt 0 ]; then
  echo
  echo "  Профиль команды — правила дирижёра и помощника под инструменты вашей команды:"
  echo "    0) общий — без командных настроек (подойдёт всем)"
  defn=0; i=1
  for p in "${profiles[@]}"; do echo "    $i) $(title "$p")"; [ "$p" = "$PROFILE" ] && defn=$i; i=$((i+1)); done
  while :; do
    ask "Номер" "$defn"
    if [[ "$REPLY" =~ ^[0-9]+$ ]] && [ "$REPLY" -le "${#profiles[@]}" ]; then break; fi
    warn "введи номер из списка"; [ -n "$NOTTY" ] && die "Запусти установку в терминале."
  done
  if [ "$REPLY" -eq 0 ]; then PROFILE=""; else PROFILE="${profiles[$((REPLY-1))]}"; fi
fi
cat > "$ENVF" <<EOF
# Пути orchestraByVitalik на этом маке (пишет install-mac.sh).
SQ_REPO="$REPO"
SQ_DIR="$SQ_DIR"
SQ_REPOS="$SQ_REPOS"
CLAUDE_BIN="$CLAUDE_BIN"
PROFILE="$PROFILE"
EOF
ok "папка сессий: $SQ_DIR"
ok "профиль: ${PROFILE:-общий}"

# ---------------------------------------------------------------- сборка
step "Собираю Squad (минута-две)"
mkdir -p "$HOME/bin"
(cd "$REPO" && go build -o "$HOME/bin/cs.new" .) || die "Сборка не вышла — пришли вывод выше автору."
chmod +x "$HOME/bin/cs.new" && mv -f "$HOME/bin/cs.new" "$HOME/bin/cs"
ok "~/bin/cs"
for f in contrib/sq/sq contrib/sq/orchestra-start contrib/sq/commit-density contrib/sq/localize contrib/claude-notify; do
  cp "$REPO/$f" "$HOME/bin/" && chmod +x "$HOME/bin/$(basename "$f")"
done
ok "~/bin/sq, orchestra-start, уведомления"
case ":$PATH:" in
  *":$HOME/bin:"*) ;;
  *) grep -qs 'HOME/bin' "$HOME/.zshrc" || echo 'export PATH="$HOME/bin:$PATH"' >> "$HOME/.zshrc"
     export PATH="$HOME/bin:$PATH"; ok "~/bin добавлен в PATH (~/.zshrc)";;
esac

# ---------------------------------------------------------------- дирижёр и помощник
step "Дирижёр и помощник"
L="$HOME/bin/localize"
fresh=""
[ -d "$HOME/.claude-squad/conductor" ] || fresh=1
# Новое докладывается, твоё (rules.md, журнал, состояние) остаётся; CLAUDE.md, настройки,
# скиллы и profile.md — свежие. Файлы профиля идут поверх общих.
PD="$REPO/contrib/profiles/$PROFILE"
for r in conductor assistant; do
  dst="$HOME/.claude-squad/$r"
  [ -n "$PROFILE" ] && [ -d "$PD/$r" ] && "$L" -r -n "$PD/$r" "$dst"
  "$L" -r -n "$REPO/contrib/$r" "$dst"
  "$L" -r "$REPO/contrib/$r/.claude" "$dst/.claude"
  "$L" "$REPO/contrib/$r/CLAUDE.md" "$dst/CLAUDE.md"
  if [ -n "$PROFILE" ] && [ -d "$PD/$r" ]; then
    [ -d "$PD/$r/.claude" ] && "$L" -r "$PD/$r/.claude" "$dst/.claude"
    [ -f "$PD/$r/profile.md" ] && "$L" "$PD/$r/profile.md" "$dst/profile.md"
  else
    rm -f "$dst/profile.md"
  fi
done
# Настройки Squad из профиля (чат для утреннего отчёта, доска задач, проверка VPN) — только незаданные.
if [ -n "$PROFILE" ] && [ -f "$PD/profile.json" ]; then
  /usr/bin/python3 - "$PD/profile.json" "$HOME/.claude-squad/config.json" <<'PY' && ok "настройки профиля добавлены в ~/.claude-squad/config.json"
import json, os, sys
prof = json.load(open(sys.argv[1])).get("config", {})
p = sys.argv[2]
d = json.load(open(p)) if os.path.exists(p) and os.path.getsize(p) else {}
for k, v in prof.items():
    d.setdefault(k, v)
json.dump(d, open(p, "w"), ensure_ascii=False, indent=2)
PY
fi
"$L" -r "$REPO/contrib/wrappers" "$HOME/.claude-squad/bin"
chmod +x "$HOME"/.claude-squad/bin/*/claude "$HOME"/.claude-squad/*/bin/* 2>/dev/null
ok "файлы ролей в ~/.claude-squad (пути подставлены твои)"

# ---------------------------------------------------------------- уведомления
if ! grep -qs "claude-notify" "$HOME/.claude/settings.json"; then
  step "Уведомления"
  if agree "Добавить в Claude Code уведомления на мак (готово / нужно разрешение)?"; then
    mkdir -p "$HOME/.claude"
    [ -f "$HOME/.claude/settings.json" ] && cp "$HOME/.claude/settings.json" "$HOME/.claude/settings.json.bak-orchestra"
    if /usr/bin/python3 - "$HOME/.claude/settings.json" <<'PY'
import json, os, sys
p = sys.argv[1]
d = json.load(open(p)) if os.path.exists(p) and os.path.getsize(p) else {}
h = d.setdefault("hooks", {})
def add(ev, matcher, arg):
    lst = h.setdefault(ev, [])
    cmd = "$HOME/bin/claude-notify " + arg
    if not any(cmd in json.dumps(x) for x in lst):
        lst.append({"matcher": matcher, "hooks": [{"type": "command", "command": cmd}]})
add("Stop", "", "stop")
add("Notification", "permission_prompt", "notify")
json.dump(d, open(p, "w"), ensure_ascii=False, indent=2)
PY
    then ok "хуки добавлены (прежние настройки: ~/.claude/settings.json.bak-orchestra)"
    else warn "не вышло — добавь хуки по README"; fi
  fi
fi

# ---------------------------------------------------------------- приложение
step "Приложение «Оркестр»"
"$HOME/bin/sq" --build-app 2>&1 | sed 's/^/  /'
[ -d "$HOME/Applications/Orchestra.app" ] && ok "~/Applications/Orchestra.app" \
  || warn "приложение не собралось — Squad всё равно работает: команда sq в терминале"

# ---------------------------------------------------------------- запуск
step "Запускаю Squad в фоне"
if tmux has-session -t "=orchestra" 2>/dev/null; then
  # Уже работает (это обновление): sq сам закроет Squad, проверит сборку и запустит новый.
  mkdir -p "$HOME/.claude-squad/update" && touch "$HOME/.claude-squad/update/restart"
  ok "Squad перезапустится с новой версией через пару минут (сессии Claude при этом работают)"
else
  "$HOME/bin/orchestra-start" | sed 's/^/  /'
fi
for _ in $(seq 1 40); do
  [ -n "$(find "$HOME/.claude-squad/status.json" -mtime -1m 2>/dev/null)" ] && break
  sleep 1
done
roles="$("$HOME/bin/cs" ctl list --json 2>/dev/null | /usr/bin/python3 -c '
import json, sys
try: d = json.load(sys.stdin)
except Exception: d = []
s = d.get("sessions", []) if isinstance(d, dict) else d
print(" ".join(k for k in ("conductor", "assistant") if any(isinstance(x, dict) and x.get(k) for x in s)))' 2>/dev/null)"
if [[ "$roles" != *conductor* ]]; then
  "$HOME/bin/cs" ctl new --dir "$HOME/.claude-squad/conductor" --title "Дирижёр" --conductor --color purple \
    --program "$HOME/.claude-squad/bin/conductor/claude" \
    --prompt "Прочитай CLAUDE.md и rules.md, сделай cs ctl list и кратко скажи, что видишь." >/dev/null 2>&1 \
    && ok "дирижёр запущен" || warn "дирижёра не запустил — создай его по README (раздел «Дирижёр»)"
fi
if [[ "$roles" != *assistant* ]]; then
  "$HOME/bin/cs" ctl new --dir "$HOME/.claude-squad/assistant" --title "Помощник" --assistant --color cyan \
    --program "$HOME/.claude-squad/bin/assistant/claude" >/dev/null 2>&1 \
    && ok "помощник запущен" || warn "помощника не запустил — создай его по README (раздел «Помощник»)"
fi

step "Готово"
echo "  • Открой «Оркестр» и закрепи его в Dock: ${B}open ~/Applications/Orchestra.app${N}"
echo "  • В меню «Оркестра» можно включить «Запускать Squad при входе в систему»."
echo "  • Первые сессии Claude спросят доверие к папкам — ответь прямо в окне сессии."
echo "  • Обновиться: ${B}cd \"$REPO\" && git pull && ./install-mac.sh${N}"
[ -n "$fresh" ] && echo "  • Свои правила дирижёру — в ~/.claude-squad/conductor/rules.md"
exit 0
