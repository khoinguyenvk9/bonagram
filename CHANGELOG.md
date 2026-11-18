# Changelog

Tiếng Việt: [CHANGELOG.vi.md](CHANGELOG.vi.md)

## v0.3.0

**New look** (from a design canvas)
- Graphite palette with one amber accent; "Bonagram" and the unread count above a borderless search field; a one-line chat header.
- Your messages sit in a warm block with the time and ✓✓ under it; each day starts with a "─── Hôm nay ───" line; reply quotes have a bar in the author's colour; reactions are chips; file and location cards are one line.
- A rounded message box with paperclip and send buttons; the bottom row shows key caps for what is open, spelled the macOS or Windows/Linux way, in place of the English key bar.
- A window too narrow for the list (under 76 columns at the default width), or dragging the divider all the way left, folds the chat list into a rail of coloured initials with unread counts and the name on hover.

**Messaging**
- Read marks: `✓` when sent, `✓✓` once read, updated live.
- Multi-line message box: `Alt+Enter`/`Ctrl+J` for a new line, pasted text keeps its lines, the box grows up to 6 rows, `↑` `↓` move between rows, a click places the cursor, `Ctrl+Z` undoes. The real cursor follows it for Vietnamese input methods.
- Emoji suggestions: `:` + a name (`:haha`, `:camon`), inserted with `Tab`/`Enter`.
- Right-click a message: quick reactions on the top row (one click, or keys 1–7), then reply, copy, pin, forward, edit, delete, links and files, each with a Nerd Font icon and its key (`BONAGRAM_ICONS=off` drops the icons).
- Pin and unpin messages (groups: notify or silent; private chats: for both or only for you).

**Chats**
- `Ctrl+K` also searches Telegram (people, groups, channels by name or `@username`) from 3 letters on, to message someone not in your list yet.
- Right-click a chat: pin or unpin it, mute or unmute it, delete it (your side only, or for both) or leave the group.
- Reads made on another device lower the unread counts at once.

**Reading**
- A click on a reply's quote jumps to the message it answers; a click on the pinned row jumps to the pin, then to the older ones (`Ghim 1/3`). Messages that are not loaded open with their surroundings.

**Performance**
- Faster than v0.2.1 despite the new look: redrawing a 500-message chat 1.15 ms (v0.2.1: 2.3 ms), one frame 0.10 ms (0.21 ms), a key typed into a 5,500-character draft 0.29 ms.

**Docs**
- README rewritten in both languages: highlights, a quick start, a table of contents, usage grouped by task, grouped key tables, and corrected details.

## v0.2.1

- Faster redraws: message bodies and reply quotes are drawn once and reused until they change. Redrawing a chat of 500 messages takes about the same time as in v0.1.0 (2.3 ms vs 2.1 ms in the benchmark), although every reply now shows a quote and text is formatted.
- `bench_test.go`: benchmarks of drawing a 500-message chat (`go test -run TestPerfHeap -v -bench . -benchmem`).
- Measured again on a real account (README, "Memory, CPU and disk"): idle CPU and scrolling cost are as in v0.1.0; with photo previews on, RSS is about 60 MB once a chat with photos is open (physical memory 18 to 20 MB). No leftover process after Ctrl+Q in 30 tries.
- This changelog.

## v0.2.0 (changes since v0.1.0)

**Reading**
- Every reply shows who and what it answers (`↪ Name: "text"`); a replied-to message older than the loaded ones is looked up once per page.
- In groups, a live message whose sender is missing from the update no longer shows the group's name: it takes the sender's name from earlier messages, or reads the chat again.
- Received formatting is drawn: bold, italic, underline, strike, spoiler (dimmed), inline code, mentions and hashtags.
- Links are blue, open with a click, are underlined while the mouse is over them, and are OSC 8 hyperlinks (Cmd/Ctrl+click in terminals that support it). The address behind a hidden link is written after its words.
- Files are drawn as cards with a coloured type badge, name and size; videos and audio show their length. Labels elsewhere say what the media is (`[file] a.pdf · 2.3 MB`, `[voice] 0:12`, `[location]`, `[contact]`, `[poll]`); link previews no longer add `[media]`.
- Static stickers are drawn in the message list.
- Locations are cards with a link to Google Maps.
- Messages deleted by others stay on screen until a restart (on purpose).

**Writing**
- `@` in a group suggests members (asked from the server only when `@` is typed); people without a username are mentioned with an entity.
- Formatting by marks: `**bold**`, `__italic__`, `~~strike~~`, `||spoiler||`, `` `code` ``, ```` ``` ```` blocks, `[words](https://url)`. Editing a message turns its formatting back into marks.
- `/position lat, long` (or `/vitri`, or a pasted Google Maps link) sends a location.

**Finding and copying**
- `Ctrl+F` searches the text of the messages of the open chat or topic; `Enter` on a result loads the messages around it.
- Drag the mouse over messages to select text; releasing copies it.
- `L` on a message copies its links.

**Files, images, video**
- `Enter` on a file saves it in `~/Downloads` (no overwriting), opens known harmless types and shows anything else in the file manager. Errors stay on screen; an expired file reference is refreshed and the download retried. A click on a file only selects it.
- The full-screen image viewer also works with the iTerm2 protocol (iTerm2, WezTerm, mintty, Rio, VS Code) and Sixel (foot, Windows Terminal 1.22+, Konsole, mlterm, contour). `BONAGRAM_IMAGES` accepts `kitty`, `iterm`, `sixel`, `blocks`.
- Videos and GIFs play inside the terminal with mpv when it is installed (Kitty graphics, Sixel or text cells), otherwise in the system player.

**Fixes**
- Reactions you add show at once; clicking a message selects it for actions without Ctrl+B; clicking the input box while selecting returns to typing.
- Windows: files and links open with `rundll32` (it called `xdg-open`).

**Known limits**: see "Limitations" in the README. Not tried against a real account yet: the iTerm2/Sixel viewer, mpv playback, sending a location, sending formatting, Ctrl+F, file downloads.
