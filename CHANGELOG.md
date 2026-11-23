# Changelog

All notable changes are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.0]

### Added
- Dual display mode: **fade** (multi-line, current line highlighted) and
  **ticker** (news-ticker scrolling, direction `from_right` by default or
  `from_left`).
- Dynamic lyric lookup through LRCLIB (`/api/get`, then fuzzy `/api/search`)
  with duration sanity check, title clean-up and disk cache (positive and
  negative).
- Local `.lrc` sidecar files next to the music file.
- Estimated timing for unsynced lyrics (flagged with `≈`).
- Config file with per-platform overrides (`[platform.<goos>.*]`) for colours,
  font sizes and everything else; defaults: text green, current line yellow.
- Font size through the kitty text-sizing protocol or DEC double-height lines.
- Karaoke wipe, edge fade, truecolor / 256 / 16 / no-colour output.
- Runtime keys: mode, offset, size, direction, pause, next/previous, reload.
- `-pipe`, `-json`, `-once` for Waybar, Polybar, tmux and similar.
- `-init`, `-check`, `-print-config`, `-doctor`.
- Linux, macOS, Windows, FreeBSD, OpenBSD and NetBSD builds, no dependencies.

[Unreleased]: https://github.com/cumulus13/mpd-lyrics/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/cumulus13/mpd-lyrics/releases/tag/v0.1.0
