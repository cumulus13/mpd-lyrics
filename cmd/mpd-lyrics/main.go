// Command mpd-lyrics shows synchronised lyrics for the song playing in MPD,
// fetched dynamically from LRCLIB.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"syscall"

	"github.com/cumulus13/mpd-lyrics/internal/app"
	"github.com/cumulus13/mpd-lyrics/internal/config"
)

// version is set at build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

const usage = `mpd-lyrics - synchronised lyrics for MPD, powered by LRCLIB

Usage:
  mpd-lyrics [flags]

Modes:
  fade     several lines, current line highlighted, others fade out (default)
  ticker   news-ticker style horizontal scrolling text

Flags:
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mpd-lyrics", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, usage)
		fs.PrintDefaults()
	}
	var (
		cfgPath  = fs.String("config", "", "path to config file (default: platform config dir, or $MPD_LYRICS_CONFIG)")
		mode     = fs.String("mode", "", "display mode: fade | ticker (overrides config)")
		pipe     = fs.Bool("pipe", false, "print the current lyric line to stdout whenever it changes (no UI)")
		jsonOut  = fs.Bool("json", false, "with -pipe: emit JSON objects for Waybar/Polybar custom modules")
		once     = fs.Bool("once", false, "with -pipe: print the current line once and exit")
		noCache  = fs.Bool("no-cache", false, "do not read or write the LRCLIB disk cache")
		offset   = fs.Int("offset", 0, "lyric offset in ms, positive = earlier (overrides general.offset_ms)")
		host     = fs.String("host", "", "MPD host or unix socket path (overrides config and $MPD_HOST)")
		port     = fs.Int("port", 0, "MPD port (overrides config and $MPD_PORT)")
		doInit   = fs.Bool("init", false, "write a commented default config file and exit")
		force    = fs.Bool("force", false, "with -init: overwrite an existing file")
		printCfg = fs.Bool("print-config", false, "print the default config to stdout and exit")
		check    = fs.Bool("check", false, "validate the config file and exit")
		doctor   = fs.Bool("doctor", false, "diagnose config, MPD connection and lyric lookup, then exit")
		showVer  = fs.Bool("version", false, "print version and exit")
	)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	switch {
	case *showVer:
		fmt.Fprintf(stdout, "mpd-lyrics %s (%s, %s/%s)\n", versionString(), runtime.Version(), runtime.GOOS, runtime.GOARCH)
		return 0
	case *printCfg:
		fmt.Fprint(stdout, config.Template())
		return 0
	case *doInit:
		return initConfig(*cfgPath, *force, stdout, stderr)
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(stderr, "mpd-lyrics: %v\n", err)
		return 2
	}
	for _, w := range cfg.Warnings {
		fmt.Fprintf(stderr, "mpd-lyrics: warning: %s\n", w)
	}
	if *check {
		src := cfg.Path
		if src == "" {
			src = "built-in defaults (no config file found)"
		}
		fmt.Fprintf(stdout, "config OK: %s\n", src)
		return 0
	}

	// Flags override the file.
	if set["mode"] {
		switch *mode {
		case "fade", "ticker":
			cfg.Mode = *mode
		case "text":
			cfg.Mode = config.ModeTicker
		default:
			fmt.Fprintf(stderr, "mpd-lyrics: -mode must be fade or ticker, got %q\n", *mode)
			return 2
		}
	}
	if set["offset"] {
		cfg.OffsetMS = *offset
	}
	if set["host"] {
		cfg.MPD.Host = *host
	}
	if set["port"] {
		cfg.MPD.Port = *port
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opt := app.Options{
		NoCache: *noCache,
		Pipe:    *pipe || *jsonOut || *once,
		JSON:    *jsonOut,
		Once:    *once,
		Version: versionString(),
		Stdout:  stdout,
		Stderr:  stderr,
	}
	if *doctor {
		if err := app.Doctor(ctx, cfg, opt); err != nil {
			fmt.Fprintf(stderr, "mpd-lyrics: %v\n", err)
			return 1
		}
		return 0
	}
	if err := app.Run(ctx, cfg, opt); err != nil {
		fmt.Fprintf(stderr, "mpd-lyrics: %v\n", err)
		return 1
	}
	return 0
}

func initConfig(path string, force bool, stdout, stderr io.Writer) int {
	if path == "" {
		path = config.DefaultPath()
	}
	if _, err := os.Stat(path); err == nil && !force {
		fmt.Fprintf(stderr, "mpd-lyrics: %s already exists (use -force to overwrite)\n", path)
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintf(stderr, "mpd-lyrics: %v\n", err)
		return 1
	}
	if err := os.WriteFile(path, []byte(config.Template()), 0o644); err != nil {
		fmt.Fprintf(stderr, "mpd-lyrics: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "wrote %s\n", path)
	return 0
}

func versionString() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return version
}
