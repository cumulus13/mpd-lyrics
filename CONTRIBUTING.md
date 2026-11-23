# Contributing

Thanks for helping! The project is intentionally small and **dependency-free** (Go standard library only).

## Setup

```sh
git clone https://github.com/cumulus13/mpd-lyrics && cd mpd-lyrics
make check        # gofmt check, go vet, tests
make race         # tests with the race detector
make build
```

Go 1.22+ is required. Use `make cross` to compile all release targets.

## Guidelines

- Keep `go.mod` free of `require` lines (CI enforces this).
- Layout code in `internal/ui` must stay pure (no I/O) and covered by tests.
- Changing a config option: update `internal/config/default.toml`, run `make sync-config`, update
  `docs/CONFIGURATION.md`, add a test in `internal/config`.
- Terminal behaviour that differs per OS goes in the `internal/termx` build-tagged files.
- Diagrams in `docs/` and `README.md` are Mermaid; CI renders every block, so syntax errors fail the build.
- Add an entry under *Unreleased* in `CHANGELOG.md`.

## Tests

| Area | How it is tested |
| --- | --- |
| LRC parsing, offsets, estimation | table tests |
| Config | template round-trip, platform precedence, validation errors |
| LRCLIB | `httptest` server: exact/search/fallback/cache/negative cache/transient errors |
| MPD | in-process fake server speaking the protocol |
| UI | deterministic layout assertions, escape-sequence golden checks |
| App | fake MPD + fake LRCLIB driving `-pipe -once` end to end |

## Releasing

1. Move *Unreleased* notes in `CHANGELOG.md` under a new `## [x.y.z]` heading.
2. `git tag vX.Y.Z && git push origin vX.Y.Z`.
3. The `Release` workflow tests, builds 11 OS/arch targets, writes `SHA256SUMS`, and publishes the GitHub release
   using the changelog section as notes (tags containing `-` become pre-releases).


## 👤 Author
        
[Hadi Cahyadi](mailto:cumulus13@gmail.com)
    

[![Buy Me a Coffee](https://www.buymeacoffee.com/assets/img/custom_images/orange_img.png)](https://www.buymeacoffee.com/cumulus13)

[![Donate via Ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/cumulus13)
 
[Support me on Patreon](https://www.patreon.com/cumulus13)