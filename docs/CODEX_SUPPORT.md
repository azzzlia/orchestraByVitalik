# Поддержка Codex в этом форке Claude Squad

Этот файл — задание для агента (Codex или Claude), который адаптирует форк под
Codex CLI. Отдай его агенту целиком: «Сделай то, что описано в docs/CODEX_SUPPORT.md».

## Что уже работает с Codex без изменений
Upstream Claude Squad запускает любую программу: `cs -p codex` или профиль в
`~/.claude-squad/config.json`. Список сессий, превью, worktree, папки-площадки,
быстрый ответ `i`, обзор `v`, сводка `s`, переименование `R`, сортировка,
`cs ctl list/read/send/new/kill` — работают для любой программы.

## Что завязано на Claude Code и требует адаптации

| Что | Где в коде | Что сделать для Codex |
|---|---|---|
| Распознавание экрана: поле ввода `❯`, recap (`recap:`), запрос разрешения, «Interrupted», черновик и серая подсказка | `session/activity.go` (`needsInputMarkers`, `interruptedMarkers`, `extractRecap`, `findInputLine`, `extractDraft`, `extractSuggestion`, `isGhostText`), `session/exchange.go` (`isUserPromptLine`, `noiseRes`, `parseExchange`) | Снять реальные экраны Codex (`tmux capture-pane -p -e -J -t <сессия>:`) в состояниях: ждёт ввода, работает, запрос подтверждения, прерван, черновик в поле. Добавить маркеры Codex, выбирая набор по программе сессии (`Instance.Program`) |
| Определение «агент — Claude» | `session/tmux/tmux.go` (`ProgramClaude`, `HasUpdatedWithContent`, `CheckAndHandleTrustPrompt`), `session/instance.go` (`startAgent`) | Добавить признак Codex (`ProgramCodex` уже есть в upstream — проверить) и ветки для него |
| Точное возобновление разговора после сна/перезагрузки | `session/instance.go` (`ClaudeConversationID`, `startAgent` — дописывает `--resume <id>`), id пишет хук `contrib/claude-notify` в `~/.claude-squad/claude-sessions/<tmux-имя>` | Узнать, как Codex CLI возобновляет конкретную сессию и откуда взять её id; писать id в тот же каталог; в `startAgent` формировать команду возобновления для Codex |
| Уведомления macOS с цветом и номером сессии | `contrib/claude-notify` (хуки Claude Code `Stop` и `Notification`) | Подключить механизм уведомлений Codex CLI (проверить актуальную документацию) к тому же скрипту или его копии: те же заголовок `🟢 #N название`, звуки, запись id разговора |
| Обёртки запуска | `contrib/wrappers/*/claude` | При необходимости — аналогичные обёртки для Codex. Имя файла должно оканчиваться на имя программы, по нему Squad узнаёт агента |
| Дирижёр | `contrib/conductor/` | Дирижёр сам может остаться на Claude; воркеры на Codex управляются через `cs ctl`, если статусы распознаются |

## Порядок работы
1. Сверь актуальную документацию Codex CLI: подтверждения, возобновление сессий,
   уведомления/хуки, как выглядит поле ввода. Не опирайся на память.
2. Запусти Codex в tmux, сними экраны во всех состояниях, сохрани их как
   фикстуры в `session/testdata/codex/`.
3. Добавь распознавание по программе сессии, не ломая Claude: все существующие
   тесты должны проходить (`go test ./...`).
4. Напиши тесты на фикстуры Codex: статус, стадия по recap/итоговому сообщению,
   запрос подтверждения, черновик, разбор переписки для окна `i`.
5. Подключи уведомления и возобновление разговора, проверь вживую: сон
   сессии → `r` → тот же разговор.

## Инварианты, которые нельзя нарушать
- Папки пользователя (external worktrees) Squad никогда не удаляет, не
  коммитит в них, не делает `git add`, не пушит.
- `cs ctl send` не печатает в сессию, где открыт запрос подтверждения или есть
  черновик пользователя.
- Номера сессий постоянные; имя tmux-сессии вычисляется `toClaudeSquadTmuxName`
  (в `contrib/claude-notify` — та же логика на Python, должна совпадать).

## Сборка
Нужен Go. `go build -o cs .` (для macOS arm64: `GOOS=darwin GOARCH=arm64`).
