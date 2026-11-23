package mpd

import (
	"bufio"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeMPD speaks just enough of the protocol for the client.
func fakeMPD(t *testing.T, password string) (addr string, cmds chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	cmds = make(chan string, 32)
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
				inList := false
				var list []string
				for {
					line, err := rd.ReadString('\n')
					if err != nil {
						return
					}
					line = strings.TrimSpace(line)
					cmds <- line
					switch {
					case line == "command_list_ok_begin":
						inList, list = true, nil
					case line == "command_list_end":
						for _, c := range list {
							switch c {
							case "status":
								conn.Write([]byte("volume: 100\nstate: play\nsongid: 12\nelapsed: 42.250\nduration: 201.5\nlist_OK\n"))
							case "currentsong":
								conn.Write([]byte("file: a/b/My Song.flac\nArtist: Band\nArtist: Other\nTitle: My Song\nAlbum: LP\nduration: 201.500\nlist_OK\n"))
							}
						}
						conn.Write([]byte("OK\n"))
						inList = false
					case inList:
						list = append(list, line)
					case strings.HasPrefix(line, "password "):
						if got, err := strconv.Unquote(strings.TrimPrefix(line, "password ")); err == nil && got == password {
							conn.Write([]byte("OK\n"))
						} else {
							conn.Write([]byte("ACK [3@0] {password} incorrect password\n"))
						}
					case line == "pause":
						conn.Write([]byte("OK\n"))
					default:
						conn.Write([]byte("ACK [5@0] {} unknown command\n"))
					}
				}
			}()
		}
	}()
	return ln.Addr().String(), cmds
}

func TestPollAndCommands(t *testing.T) {
	addr, cmds := fakeMPD(t, "")
	c, err := Dial("tcp", addr, "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	st, song, err := c.Poll()
	if err != nil {
		t.Fatal(err)
	}
	if st.State != "play" || st.Elapsed != 42.25 || st.Duration != 201.5 || st.SongID != 12 {
		t.Errorf("status = %+v", st)
	}
	if song.Title != "My Song" || song.Artist != "Band" || song.Album != "LP" || song.Duration != 201.5 {
		t.Errorf("song = %+v", song)
	}
	if err := c.Pause(); err != nil {
		t.Fatal(err)
	}
	if err := c.Next(); err == nil {
		t.Error("expected ACK error for unknown command")
	}
	var seen []string
	for len(cmds) > 0 {
		seen = append(seen, <-cmds)
	}
	if !strings.Contains(strings.Join(seen, ","), "pause") {
		t.Errorf("pause not sent: %v", seen)
	}
}

func TestPassword(t *testing.T) {
	addr, _ := fakeMPD(t, `se"cret`)
	if _, err := Dial("tcp", addr, "wrong", time.Second); err == nil {
		t.Error("wrong password accepted")
	}
	c, err := Dial("tcp", addr, `se"cret`, time.Second)
	if err != nil {
		t.Fatalf("good password rejected: %v", err)
	}
	c.Close()
}

func TestParseSongFallbacks(t *testing.T) {
	s := ParseSong([]kv{{"file", `dir\sub/Track 01.mp3`}, {"Time", "180"}, {"AlbumArtist", "AA"}})
	if s.Title != "Track 01" || s.Duration != 180 || s.Artist != "AA" {
		t.Errorf("song = %+v", s)
	}
	if !ParseSong(nil).Empty() {
		t.Error("empty song")
	}
}

func TestParseStatusTimeFallback(t *testing.T) {
	st := ParseStatus([]kv{{"state", "pause"}, {"time", "30:200"}})
	if st.Elapsed != 30 || st.Duration != 200 || st.State != "pause" {
		t.Errorf("status = %+v", st)
	}
}

func TestResolve(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	n, a, p := Resolve("", 0, "", env(nil))
	if n != "tcp" || a != "localhost:6600" || p != "" {
		t.Errorf("defaults = %s %s %q", n, a, p)
	}
	n, a, p = Resolve("", 0, "", env(map[string]string{"MPD_HOST": "pw@music.lan", "MPD_PORT": "6601"}))
	if n != "tcp" || a != "music.lan:6601" || p != "pw" {
		t.Errorf("env = %s %s %q", n, a, p)
	}
	n, a, _ = Resolve("/run/mpd/socket", 0, "", env(map[string]string{"MPD_HOST": "ignored"}))
	if n != "unix" || a != "/run/mpd/socket" {
		t.Errorf("unix = %s %s", n, a)
	}
	n, a, p = Resolve("h", 7000, "x", env(map[string]string{"MPD_HOST": "pw@other"}))
	if a != "h:7000" || p != "x" {
		t.Errorf("explicit = %s %s %q", n, a, p)
	}
}
