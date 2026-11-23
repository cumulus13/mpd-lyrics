# Architecture

mpd-lyrics is a set of small packages under `internal/`, using only the Go standard library.

| Package | Responsibility |
| --- | --- |
| `mpd` | MPD text protocol: connect, password, `status` + `currentsong` in one round trip, pause/next/previous |
| `lrclib` | LRCLIB client: exact get, fuzzy search, candidate scoring, title clean-up, disk cache |
| `lrc` | LRC parser (multi-stamp, `[offset:]`, word tags), plain-text lyrics, timing estimation, line lookup |
| `config` | TOML-subset parser, platform overrides, validation, embedded default file |
| `colorx` | colour parsing, interpolation, truecolor/256/16 down-conversion, terminal detection |
| `termx` | terminal size, raw mode, VT enabling (Windows), key decoding, cell width of CJK/emoji |
| `ui` | pure layout (`Frame → []Row`) for both modes, and the diffing ANSI writer (`Screen`) |
| `app` | engine: poller, lyric fetching, clock, key handling, pipe mode, doctor |

## Components

```mermaid
flowchart TB
    subgraph cmd[cmd/mpd-lyrics]
        main[main.go flags and exit codes]
    end
    subgraph app[internal/app]
        engine[engine: model and clock]
        tui[TUI loop]
        pipe[pipe mode]
        doctor[doctor]
    end
    subgraph ui[internal/ui]
        layout[Renderer: fade, ticker, chrome]
        screen[Screen: diff and escape codes]
    end
    main --> config[internal/config]
    main --> engine
    engine --> mpd[internal/mpd]
    engine --> lrclib[internal/lrclib]
    engine --> lrc[internal/lrc]
    lrclib --> cache[(JSON cache files)]
    tui --> layout --> screen
    tui --> termx[internal/termx]
    layout --> colorx[internal/colorx]
    engine --> tui
    engine --> pipe
    engine --> doctor
```

## Runtime flow

The UI never blocks on the network. The poller and the lyric fetcher run in goroutines and hand results to the
single-threaded engine through channels; the engine owns all state.

```mermaid
sequenceDiagram
    autonumber
    participant M as MPD
    participant P as Poller goroutine
    participant E as Engine (UI loop)
    participant F as Fetch goroutine
    participant L as LRCLIB
    loop every poll_interval_ms
        P->>M: command_list: status, currentsong
        M-->>P: state, elapsed, song tags
        P->>E: snapshot (latest wins)
    end
    E->>E: song key changed?
    E->>F: start fetch (cancels previous)
    F->>F: local .lrc sidecar?
    F->>L: GET /api/get, then /api/search
    L-->>F: synced / plain lyrics
    F->>E: lyricsMsg (ignored if song changed meanwhile)
    loop every frame (fps)
        E->>E: lyric time = interpolated position + offsets
        E->>E: Render(Frame) then Screen.Draw (changed rows only)
    end
```

### Playback clock

MPD is polled every 300 ms but frames are drawn at 30 fps, so the position is extrapolated
(`base + now − pollTime`) while playing. A new poll within 250 ms of the prediction nudges the clock by half the
error (no visible jumps); a larger error (seek) snaps to the new value. Pausing freezes it.

## Lyric resolution

```mermaid
flowchart TD
    S[Song changed] --> A{music_dir set and prefer_local?}
    A -- yes --> B{sidecar .lrc parses?}
    B -- yes --> OK[Synced lyrics, source local]
    A -- no --> C
    B -- no --> C{cache hit and fresh?}
    C -- found --> OKR[Use cached record]
    C -- not found, fresh --> NF[No lyrics found]
    C -- miss or stale --> D[GET /api/get: title, artist, album, duration]
    D -- 200 --> OKR
    D -- 404 --> E[GET /api/search, then retry with cleaned title]
    E --> F{candidate within max_duration_diff_s?}
    F -- yes --> G[Pick best: synced, closest duration, names match]
    G --> OKR
    F -- none --> NF
    D -- network or 5xx error --> ERR[Show error, do not cache]
    E -- network or 5xx error --> ERR
    OKR --> H{synced lyrics?}
    H -- yes --> OK2[Parse LRC, source lrclib]
    H -- no, plain only --> I{unsynced_fallback = estimate?}
    I -- yes --> EST[Estimate timing, header shows ≈]
    I -- no --> NS[Only unsynced lyrics available]
```

## UI state machine

```mermaid
stateDiagram-v2
    [*] --> Connecting
    Connecting --> NothingPlaying: connected, stopped
    Connecting --> Fetching: connected, song loaded
    NothingPlaying --> Fetching: song starts
    Fetching --> Showing: lyrics found
    Fetching --> NoLyrics: not found or failed
    Showing --> Fetching: song changed
    NoLyrics --> Fetching: song changed or r pressed
    Showing --> Showing: r pressed (refetch)
    Showing --> Connecting: connection lost
    NoLyrics --> Connecting: connection lost
    Connecting --> Connecting: retry with backoff 1s to 5s
```

## Rendering

`ui.Renderer.Render(Frame) → []Row` is pure: same frame, same rows. That is what the unit tests assert (centring,
colours, cross-fade blends, ticker anchor and direction, wide-character clipping). `ui.Screen` encodes rows to
escape sequences, writes only rows that changed, and wraps each update in synchronized-output markers
(`CSI ? 2026 h/l`, ignored by terminals that do not support them) to avoid tearing.

### Ticker geometry

Let `W` be the width, `a = anchor·W`, line `i` start time `t_i`, and `adv_i` the tape distance of line `i`
(text width + separator width, scaled). The tape position `P(t)` is piecewise linear:

```
P(t_i) = Σ adv_j  for j < i
P(t) between t_i and t_{i+1} advances by exactly adv_i
```

The text of line `i` starts at screen column

```
from_right:  x_i(t) = (W − a) + P(t_i) − P(t)
from_left:   x_i(t) = a + (P(t) − P(t_i)) − textWidth_i      (text ends at the anchor)
```

so at `t = t_i`, `x_i = W − a`, i.e. the active line is always exactly at the anchor when its timestamp arrives, and
the speed on each segment is `adv_i / (t_{i+1} − t_i)`.

```mermaid
flowchart LR
    subgraph from_right
        direction LR
        r1[previous line] --- r2[current line starts at W-a] --- r3[next line enters from right]
    end
    subgraph from_left
        direction LR
        l1[next line enters from left] --- l2[current line ends at a] --- l3[previous line]
    end
```

### Font size

`Row` carries `Scale` and `Part`. With `kitty`, a scaled run is emitted as `OSC 66 ; s=N ; text ST` on the first row
of its block (the terminal covers the following rows). With `dec`, rows are prefixed with `ESC # 3` (top half) and
`ESC # 4` (bottom half) so each glyph is double width and double height. Layout always reserves `Scale` rows per
scaled line, so the fade list stays centred.

## Design decisions

- **Own TOML subset instead of a library**: the build has no dependencies, and the subset covers all options.
- **Own MPD client**: only `status`, `currentsong`, `pause`, `next`, `previous` and `password` are needed.
- **Position interpolation**: smooth animation at any fps without hammering MPD.
- **Negative cache with a short TTL**: polite to LRCLIB, still picks up new uploads within a day.
- **No colour fallback logic in layouts**: layout produces RGB, `colorx.Profile` down-converts at the edge.

## 👤 Author
        
[Hadi Cahyadi](mailto:cumulus13@gmail.com)
    

[![Buy Me a Coffee](https://www.buymeacoffee.com/assets/img/custom_images/orange_img.png)](https://www.buymeacoffee.com/cumulus13)

[![Donate via Ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/cumulus13)
 
[Support me on Patreon](https://www.patreon.com/cumulus13)