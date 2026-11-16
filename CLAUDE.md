# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`bonagram` is a terminal Telegram client in Go: [gotd/td](https://github.com/gotd/td) (MTProto) + Bubble Tea v1. User-facing docs (keys, config, requirements, limits) are in `README.md` (English) and `README.vi.md` (Vietnamese); keep both in sync when behaviour changes. The UI text is Vietnamese. The look is a fixed dark theme (colour constants at the top of `view.go`).

## Commands

```bash
go build -ldflags="-s -w" -o bonagram .   # the binary the user runs (./bonagram); needs Go >= 1.26 (the go.mod minimum comes from the dependencies)
go vet ./... && go test -race ./...       # all tests; none touch the network
go test -run TestModelFlow ./...          # one test
```

There is no linter config beyond `gofmt`/`go vet`. Tests use fake data only: `bonagram_test.go` drives the Bubble Tea model by sending messages to `Update` and checking `View()`; it never connects to Telegram. Anything that calls the API (`loadChats`, `loadMessages`, sending, marking read) is only verifiable by running the app against a real account.

First run asks for `api_id`/`api_hash`, then the phone/code login on plain stdin. Config and session live in `~/.config/bonagram/` (`env`, `session.json` = full account access, `width`).

## Architecture

Single `package main`; the pieces that must be read together:

- **`main.go`**: wiring. `gotd` client → `updates.Manager` (`gaps`) → `tg.UpdateDispatcher` → handlers push `newMsg`/`reactMsg` into a buffered `events` channel. The model drains that channel with the `wait()` command, which **must be re-armed by every handler of an event from the channel** (`onNew`, `onReact`), otherwise live updates silently stop. The Bubble Tea program runs in a goroutine; `gaps.Run` blocks the main one; quitting the UI cancels the shared context.
- **`tg.go`**: data types (`chat`, `line`, `topic`, `reaction`) and everything that talks to the API (`loadChats`, `loadMessages`, `loadTopics`, `markRead`, `sendText`). All text from Telegram goes through `clean()` (strips control, private-use, format and variation characters) so it cannot inject escape sequences or break width calculations.
- **`ui.go`**: the Bubble Tea `model`: state, `Update`, key/mouse handling, commands that return `...Msg` results. Value-receiver `Update`/`View`, but mutations happen through pointer methods on the local copy (`m.update`). Messages are cached per view in `m.msgs`, keyed by chat key, or `chat#topic` in forums (`mkFor`).
- **`view.go`**: pure rendering. The whole frame is built line by line and every line goes through `paint()` (re-applies colours after each reset and pads to the exact width). The frame must be exactly `h` lines of `w` cells; tests assert this. `render()` also records `lineAt` (first content line of each message) and `hits` (copy buttons), which mouse handling reads.
- **`code.go`**: code blocks and the copy button (entity offsets are UTF-16 units, hence `segments`), clipboard via OSC 52 + `pbcopy`/`wl-copy`/`xclip`.
- **`media.go`**: downloads, image drawing (Kitty unicode placeholders, or half-blocks fallback; `BONAGRAM_IMAGES` forces one), and `lockedOut`/`termWrite`.
- **`input.go`**: filter that repairs mouse reports Bubble Tea split in two (they would otherwise be typed into the input box).

## Non-obvious things

- **All terminal output goes through one mutex** (`lockedOut` is Bubble Tea's output; image/bell/OSC writes use `termWrite`). Each Bubble Tea frame is wrapped in autowrap-off (`?7l`…`?7h`) and followed by a move of the **real** cursor to the focused input, because IME composing text (Vietnamese Telex/VNI) is drawn at the real cursor. The built-in virtual cursor of the text inputs is disabled for that reason. `lockedOut` must keep `Read/Fd/Close`, or Bubble Tea stops recognising the terminal and the screen stays blank.
- **Widths**: use `ansi.StringWidth`/`ansi.Truncate` (grapheme-aware), never `runewidth`, for anything that is laid out; mixing the two shifted rows and overlapped chats.
- **gotd quirks**: `FolderID(0)` means "unset", so `GetDialogs` returns archived chats too; filter on `Dialog.FolderID != 0`. The updates manager only follows channels it was told about, so `loadChats` returns `chanTrack`s that are registered with `TrackChannel` (otherwise group messages arrive late or not at all). The manager runs on in-memory state only.
- **Forum unread**: the server keeps a per-forum unread count that is not lowered when a topic is read (seen stuck at 7 while every topic said 0), so the badge of a forum is the sum of its topics' counts (`sumUnread`); `loadChats` fetches the topics of every forum for that, and `open` refetches them. Reading a topic is `messages.readDiscussion(peer, topic id, up to message id)` (verified accepted by the server).
- **Message actions** (`actions.go` = requests, `msgactions.go` = keys and results): edit/delete/react/forward/open link/jump to mention are keys of browse mode (`^B`); `/file <path>` in the input uploads; the pinned message is one `messages.search(filter=pinned)` per `enter`; drafts are kept per `chat#topic` in `m.drafts`. The row above the input is `barText()` (prompt, edit, or reply) and `msgY()` counts the pinned row: both feed `layout()`, so call `layout()` after changing them. Gotchas found against the real account: (1) results of our own sends/uploads/forwards are **not** announced back as updates, so `sendText` returns the id from the reply (`sentID`, `numberSent`) to make a just-sent line editable, and uploads/forwards refetch the open chat (`refresh`); (2) in Saved Messages the server does not mark messages `out`, hence `mine()` uses `selfKey`; (3) delete only own messages, never others' (`Revoke` would remove them for the other person too); (4) forwards go only where the user picks, and live tests of write actions belong in Saved Messages only (the user said: never forward to anyone else).
- **Pinned chats** (`chat.pinned`, from `Dialog.Pinned`) are sorted first at load and `placed()` keeps them above activity: a new message lifts a chat only to the top of the unpinned ones, a pinned chat stays in its slot. Pin changes made while the app runs are not followed.
- **CLI and help** (`help.go`): `cli()` handles `--version/--help` before anything starts (unknown option: exit 2); `version` is set with `-ldflags "-X main.version=..."`. The in-app guide is `m.help` (F1; scrolls with arrows/wheel), its text is `helpDoc`: update it whenever keys change. The first launch opens it once (marker file `help-seen` in the config dir). Keep guide rows short (about 56 characters): longer ones are cut in an 80-column window.
- **Reads happen on open**: opening a chat with unread messages marks it read on the server. When trying the app against a real account, pick chats without unread messages, and do not message other people; Saved Messages ("me") is the safe place for live tests (delete the test message afterwards).
- The renderer frame rate is `BONAGRAM_FPS` (default 60). It was tried at 30 (idle CPU 0.67% -> 0.41%) but the user found typing/scrolling slightly slower, so 60 stays the default; do not lower it without asking. `GOMAXPROCS` made no difference to CPU.
- Shutdown: SIGHUP/SIGTERM are handled like Ctrl+C, and from the moment quitting starts (QuitMsg or context cancelled) a 3 s `os.Exit` timer guards against a wedged exit (a leftover process was seen twice after Ctrl+Q + closing the terminal, cause not found, not reproducible in ~10 tries). Always check for leftover `bonagram` processes after testing in tmux.
- Edit updates (`OnEditMessage`/`OnEditChannelMessage` → `editMsg` → `onEdit`) are what makes a bot's streamed answer (one message edited many times) show in full; like reactions they redraw at most every 250 ms and `onEdit` must re-arm `wait()`. Deletions by others are still not handled.
- Reaction updates redraw at most every 250 ms (`renderTick`) and only when the message is in the open view; don't redraw per update.
