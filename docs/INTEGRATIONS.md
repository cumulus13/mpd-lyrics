# Integrations

`-pipe` turns mpd-lyrics into a headless source of the current lyric line. Without `-once` it keeps running and
prints a new line **only when the line (or state) changes**; with `-once` it waits until MPD and the lyrics are
known, prints once and exits (the lookup is cached, so repeated calls are cheap).

`-json` prints one object per change:

```json
{"text":"I've come to talk with you again","tooltip":"Simon & Garfunkel — The Sound of Silence","class":"playing","alt":"playing"}
```

`class` / `alt` is one of `playing`, `paused`, `stopped`, `nolyrics`, `disconnected`. `text` is empty when there is
no active line.

## Waybar

```jsonc
"custom/lyrics": {
    "exec": "mpd-lyrics -json",
    "return-type": "json",
    "format": "♪ {}",
    "max-length": 60,
    "escape": true,
    "tooltip": true
}
```

```css
#custom-lyrics.paused   { opacity: 0.5; }
#custom-lyrics.nolyrics { opacity: 0.3; }
```

## Polybar

```ini
[module/lyrics]
type = custom/script
exec = mpd-lyrics -pipe
tail = true
format-prefix = "♪ "
```

## tmux

```tmux
set -g status-interval 1
set -g status-right '#(mpd-lyrics -once | cut -c1-60)'
```

Each refresh starts a short-lived process that polls MPD once (lyrics come from the cache after the first lookup).
For a smoother result keep one process running and share its output through a file:

```sh
mpd-lyrics -pipe | while read -r l; do printf '%s' "$l" > /tmp/lyric; done &
```

```tmux
set -g status-right '#(cat /tmp/lyric | cut -c1-60)'
```

## Sway / i3 scratchpad

Run the full-screen UI in a floating terminal:

```
# sway
for_window [app_id="lyrics"] floating enable, resize set 900 220, move position center
bindsym $mod+y exec foot --app-id lyrics mpd-lyrics -mode ticker
```

## Latency compensation

Bluetooth headphones or network streams delay the audio relative to MPD's reported position. Set
`general.offset_ms` (positive = lyrics earlier), or tune live with `+` / `-` and note the value shown in the header.

## 👤 Author
        
[Hadi Cahyadi](mailto:cumulus13@gmail.com)
    

[![Buy Me a Coffee](https://www.buymeacoffee.com/assets/img/custom_images/orange_img.png)](https://www.buymeacoffee.com/cumulus13)

[![Donate via Ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/cumulus13)
 
[Support me on Patreon](https://www.patreon.com/cumulus13)