# Changelog

Tiếng Việt: [CHANGELOG.vi.md](CHANGELOG.vi.md)

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
