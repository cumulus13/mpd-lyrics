# Usage

## Modes

### `fade`

Shows `fade.lines` lines around the current one. The current line is vertically centred and drawn in
`current_color`; neighbours blend from `text_color` towards `fade_to` with distance (down to `fade.min_alpha`).
When the line changes, the old line cross-fades out and the new one fades in over `fade.fade_ms`.

Options: `style.wipe = true` adds a karaoke wipe, where the already-sung part of the current line uses
`current_color` and the rest keeps `text_color`.

### `ticker`

All lyric lines form one continuous tape separated by `ticker.separator`. The tape always moves, in the direction
set by `ticker.direction`:

| `direction` | Motion | Entry edge |
| --- | --- | --- |
| `from_right` (default) | text moves right → left | right |
| `from_left` | text moves left → right | left |

The tape speed is adjusted per line so that **a line starts exactly at the anchor position
(`ticker.anchor`, a fraction of the width measured from the entry edge) at the instant its timestamp is reached**.
Short lines therefore scroll slowly and long lines faster, and the highlight never drifts out of sync. Before the
first and after the last line the tape keeps rolling at a gentle speed. See
[ARCHITECTURE.md](ARCHITECTURE.md#ticker-geometry) for the maths.

## Lyric sources

In order (when `lrclib.prefer_local = true`, the default):

1. **Local sidecar**: `<mpd.music_dir>/<same path as song>.lrc` (needs `mpd.music_dir`).
2. **LRCLIB exact match** (`/api/get`), using title, artist, album and duration.
3. **LRCLIB search** (`/api/search`), also retried with a cleaned title (`(feat. X)`, `[Official Video]`,
   `- 2011 Remaster` removed). Candidates whose length differs from the track by more than
   `lrclib.max_duration_diff_s` are rejected; synced lyrics and closest duration win.
4. **Unsynced lyrics**: with `unsynced_fallback = "estimate"` the lines are spread over the track length, weighted by
   line length. The header shows `≈ estimated`; treat the timing as approximate. `"off"` disables this.

Network failures are shown on screen and **never cached**; "not found" is cached for `not_found_ttl_hours`.
Press `r` to force a fresh lookup.

## Command line

| Flag | Description |
| --- | --- |
| `-config PATH` | config file (also `$MPD_LYRICS_CONFIG`) |
| `-mode fade\|ticker` | override `general.mode` |
| `-offset MS` | override `general.offset_ms` |
| `-host`, `-port` | MPD address (host may be a unix socket path) |
| `-no-cache` | bypass the LRCLIB disk cache |
| `-pipe` | no UI: print the current line each time it changes |
| `-json` | with `-pipe`: JSON objects (`text`, `tooltip`, `class`, `alt`) |
| `-once` | with `-pipe`: print once and exit (for status bars that poll) |
| `-init [-force]` | write the default config to the platform location |
| `-print-config` | print the default config to stdout |
| `-check` | validate the config and exit |
| `-doctor` | diagnose config, MPD and a lookup for the current song |
| `-version` | print the version |

Exit codes: `0` success, `1` runtime error, `2` bad flags or config.

## Config file location

| Platform | Path |
| --- | --- |
| Linux, BSD | `$XDG_CONFIG_HOME/mpd-lyrics/config.toml` (default `~/.config/...`) |
| macOS | `~/Library/Application Support/mpd-lyrics/config.toml`, then the XDG path |
| Windows | `%APPDATA%\mpd-lyrics\config.toml` |

An explicit `-config` or `$MPD_LYRICS_CONFIG` must exist. Without a file, built-in defaults apply.

## Font size in a terminal

A terminal cell grid has one font size, so "font size" is realised by scaling glyphs where the terminal allows it:

| `scale_method` | Mechanism | Scales | Terminals |
| --- | --- | --- | --- |
| `kitty` | text-sizing protocol (OSC 66) | 1–7 | kitty ≥ 0.40 and others implementing it |
| `dec` | DEC double-width/double-height lines | 1–2 | xterm, mlterm and compatibles |
| `none` | disabled | 1 | everything else |
| `auto` (default) | `kitty` when running in kitty, else `none` | | |

Unsupported terminals show scaled text as plain text, so only enable this where you know it works. Applying a
terminal-wide font size is not possible from inside a terminal program; change it in your terminal's own settings.

## 👤 Author
        
[Hadi Cahyadi](mailto:cumulus13@gmail.com)
    

[![Buy Me a Coffee](https://www.buymeacoffee.com/assets/img/custom_images/orange_img.png)](https://www.buymeacoffee.com/cumulus13)

[![Donate via Ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/cumulus13)
 
[Support me on Patreon](https://www.patreon.com/cumulus13)