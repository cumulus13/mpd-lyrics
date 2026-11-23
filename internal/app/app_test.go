package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cumulus13/mpd-lyrics/internal/config"
	"github.com/cumulus13/mpd-lyrics/internal/lrclib"
	"github.com/cumulus13/mpd-lyrics/internal/mpd"
)

func fakeMPD(t *testing.T, elapsed string) (host string, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				conn.Write([]byte("OK MPD 0.23.5\n"))
				rd := bufio.NewReader(conn)
				for {
					line, err := rd.ReadString('\n')
					if err != nil {
						return
					}
					if strings.TrimSpace(line) == "command_list_end" {
						conn.Write([]byte("state: pause\nsongid: 1\nelapsed: " + elapsed + "\nduration: 100\nlist_OK\n" +
							"file: x/song.flac\nArtist: Band\nTitle: Song\nAlbum: LP\nduration: 100\nlist_OK\nOK\n"))
					}
				}
			}()
		}
	}()
	a := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", a.Port
}

func fakeLRCLIB(t *testing.T, r lrclib.Result) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/get", func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(r) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func testCfg(t *testing.T, elapsed string, res lrclib.Result) *config.Config {
	t.Helper()
	host, port := fakeMPD(t, elapsed)
	srv := fakeLRCLIB(t, res)
	cfg := config.Default()
	cfg.MPD.Host, cfg.MPD.Port = host, port
	cfg.LRCLIB.BaseURL = srv.URL
	cfg.LRCLIB.CacheDir = t.TempDir()
	return cfg
}

func runOnce(t *testing.T, cfg *config.Config, jsonOut bool) string {
	t.Helper()
	var out, errb bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := Run(ctx, cfg, Options{Pipe: true, Once: true, JSON: jsonOut, Stdout: &out, Stderr: &errb, Version: "test"})
	if err != nil {
		t.Fatalf("Run: %v (stderr %q)", err, errb.String())
	}
	return out.String()
}

func TestPipeOncePicksActiveLine(t *testing.T) {
	cfg := testCfg(t, "25.0", lrclib.Result{ID: 1, SyncedLyrics: "[00:10.00]one\n[00:20.00]two\n[00:30.00]three\n"})
	if got := strings.TrimSpace(runOnce(t, cfg, false)); got != "two" {
		t.Errorf("line = %q, want two", got)
	}
}

func TestPipeOnceOffset(t *testing.T) {
	cfg := testCfg(t, "25.0", lrclib.Result{ID: 1, SyncedLyrics: "[00:10.00]one\n[00:20.00]two\n[00:30.00]three\n"})
	cfg.OffsetMS = 6000 // lyrics earlier by 6s: 25+6 = 31s -> three
	if got := strings.TrimSpace(runOnce(t, cfg, false)); got != "three" {
		t.Errorf("line = %q, want three", got)
	}
}

func TestPipeOnceJSON(t *testing.T) {
	cfg := testCfg(t, "12.0", lrclib.Result{ID: 1, SyncedLyrics: "[00:10.00]hello\n"})
	var p pipeLine
	if err := json.Unmarshal([]byte(runOnce(t, cfg, true)), &p); err != nil {
		t.Fatal(err)
	}
	if p.Text != "hello" || p.Class != "paused" || p.Tooltip != "Band — Song" {
		t.Errorf("json = %+v", p)
	}
}

func TestUnsyncedEstimateAndOff(t *testing.T) {
	res := lrclib.Result{ID: 1, PlainLyrics: "alpha\nbeta\ngamma\ndelta"}
	cfg := testCfg(t, "90.0", res)
	if got := strings.TrimSpace(runOnce(t, cfg, false)); got != "delta" {
		t.Errorf("estimated line near end = %q, want delta", got)
	}
	cfg = testCfg(t, "50.0", res)
	cfg.LRCLIB.UnsyncedFallback = "off"
	if got := strings.TrimSpace(runOnce(t, cfg, false)); got != "" {
		t.Errorf("fallback off should show nothing, got %q", got)
	}
}

func TestLocalSidecarPreferred(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x", "song.lrc"), []byte("[00:00.00]local line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := testCfg(t, "5.0", lrclib.Result{ID: 1, SyncedLyrics: "[00:00.00]remote line\n"})
	cfg.MPD.MusicDir = dir
	if got := strings.TrimSpace(runOnce(t, cfg, false)); got != "local line" {
		t.Errorf("line = %q, want local line", got)
	}
}

func TestLocalLRCRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"../etc/passwd", "/abs/path.flac", "http://stream/x.mp3", ""} {
		if localLRC(dir, f) != nil {
			t.Errorf("localLRC accepted %q", f)
		}
	}
}

func TestClockInterpolation(t *testing.T) {
	var c clock
	t0 := time.Now()
	c.update(mpd.Status{State: "play", Elapsed: 10}, t0)
	if got := c.now(t0.Add(2 * time.Second)); got < 11.99 || got > 12.01 {
		t.Errorf("playing now = %v", got)
	}
	// small drift is nudged, not jumped
	c.update(mpd.Status{State: "play", Elapsed: 12.1}, t0.Add(2*time.Second))
	if got := c.now(t0.Add(2 * time.Second)); got <= 12.0 || got >= 12.1 {
		t.Errorf("drift correction = %v", got)
	}
	// seek: hard jump
	c.update(mpd.Status{State: "play", Elapsed: 60}, t0.Add(3*time.Second))
	if got := c.now(t0.Add(3 * time.Second)); got != 60 {
		t.Errorf("seek = %v", got)
	}
	// paused clock does not advance
	c.update(mpd.Status{State: "pause", Elapsed: 60}, t0.Add(4*time.Second))
	if got := c.now(t0.Add(9 * time.Second)); got != 60 {
		t.Errorf("paused = %v", got)
	}
}
