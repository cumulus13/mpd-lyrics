// Package mpd implements the small part of the MPD text protocol that
// mpd-lyrics needs: status polling, current song and a few playback commands.
package mpd

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// Status is the subset of "status" we use.
type Status struct {
	State    string // play | pause | stop
	Elapsed  float64
	Duration float64
	SongID   int
}

// Song is the subset of "currentsong" we use.
type Song struct {
	File     string
	Title    string
	Artist   string
	Album    string
	Duration float64
}

// Empty reports whether nothing is loaded.
func (s Song) Empty() bool { return s.File == "" && s.Title == "" }

// Key identifies a song for change detection.
func (s Song) Key() string {
	return fmt.Sprintf("%s|%s|%s|%s|%d", s.File, s.Artist, s.Title, s.Album, int(s.Duration+0.5))
}

type kv struct{ k, v string }

// Client is a single MPD connection. It is not safe for concurrent use.
type Client struct {
	network, addr, password string
	timeout                 time.Duration
	conn                    net.Conn
	rd                      *bufio.Reader
}

// Resolve turns config/env values into a dial target. Explicit values win over
// $MPD_HOST/$MPD_PORT; $MPD_HOST may be "password@host".
func Resolve(host string, port int, password string, getenv func(string) string) (network, addr, pass string) {
	pass = password
	if host == "" {
		host = getenv("MPD_HOST")
		if at := strings.LastIndex(host, "@"); at > 0 {
			if pass == "" {
				pass = host[:at]
			}
			host = host[at+1:]
		}
	}
	if host == "" {
		host = "localhost"
	}
	if strings.HasPrefix(host, "/") || strings.HasPrefix(host, "@") {
		return "unix", host, pass
	}
	if port == 0 {
		if p, err := strconv.Atoi(getenv("MPD_PORT")); err == nil && p > 0 {
			port = p
		} else {
			port = 6600
		}
	}
	return "tcp", net.JoinHostPort(host, strconv.Itoa(port)), pass
}

// Dial connects and authenticates.
func Dial(network, addr, password string, timeout time.Duration) (*Client, error) {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	conn, err := net.DialTimeout(network, addr, timeout)
	if err != nil {
		return nil, err
	}
	c := &Client{network: network, addr: addr, password: password, timeout: timeout, conn: conn, rd: bufio.NewReader(conn)}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	greeting, err := c.rd.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("reading greeting: %w", err)
	}
	if !strings.HasPrefix(greeting, "OK MPD ") {
		conn.Close()
		return nil, fmt.Errorf("unexpected greeting %q", strings.TrimSpace(greeting))
	}
	if password != "" {
		if _, err := c.Do("password " + quote(password)); err != nil {
			conn.Close()
			return nil, err
		}
	}
	return c, nil
}

// Close closes the connection.
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}

// Do runs one command and discards its output.
func (c *Client) Do(cmd string) ([]kv, error) {
	secs, err := c.roundTrip(cmd+"\n", false)
	if err != nil {
		return nil, err
	}
	if len(secs) == 0 {
		return nil, nil
	}
	return secs[0], nil
}

// Pause toggles pause. Next/Previous skip tracks.
func (c *Client) Pause() error    { _, err := c.Do("pause"); return err }
func (c *Client) Next() error     { _, err := c.Do("next"); return err }
func (c *Client) Previous() error { _, err := c.Do("previous"); return err }

// Poll fetches status and current song in a single round trip.
func (c *Client) Poll() (Status, Song, error) {
	secs, err := c.roundTrip("command_list_ok_begin\nstatus\ncurrentsong\ncommand_list_end\n", true)
	if err != nil {
		return Status{}, Song{}, err
	}
	if len(secs) < 2 {
		return Status{}, Song{}, errors.New("short response from MPD")
	}
	return ParseStatus(secs[0]), ParseSong(secs[1]), nil
}

func (c *Client) roundTrip(payload string, list bool) ([][]kv, error) {
	if c.conn == nil {
		return nil, errors.New("not connected")
	}
	_ = c.conn.SetDeadline(time.Now().Add(c.timeout))
	if _, err := c.conn.Write([]byte(payload)); err != nil {
		return nil, err
	}
	var secs [][]kv
	var cur []kv
	for {
		line, err := c.rd.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "OK":
			if !list {
				secs = append(secs, cur)
			}
			return secs, nil
		case line == "list_OK":
			secs = append(secs, cur)
			cur = nil
		case strings.HasPrefix(line, "ACK "):
			return nil, fmt.Errorf("mpd: %s", line[4:])
		default:
			if i := strings.Index(line, ": "); i > 0 {
				cur = append(cur, kv{line[:i], line[i+2:]})
			}
		}
	}
}

func first(p []kv, keys ...string) string {
	for _, k := range keys {
		for _, e := range p {
			if strings.EqualFold(e.k, k) {
				return e.v
			}
		}
	}
	return ""
}

// ParseStatus converts a "status" response.
func ParseStatus(p []kv) Status {
	st := Status{State: first(p, "state")}
	st.Elapsed, _ = strconv.ParseFloat(first(p, "elapsed"), 64)
	st.Duration, _ = strconv.ParseFloat(first(p, "duration"), 64)
	if t := first(p, "time"); t != "" && (st.Elapsed == 0 || st.Duration == 0) {
		if a, b, ok := strings.Cut(t, ":"); ok {
			if st.Elapsed == 0 {
				st.Elapsed, _ = strconv.ParseFloat(a, 64)
			}
			if st.Duration == 0 {
				st.Duration, _ = strconv.ParseFloat(b, 64)
			}
		}
	}
	st.SongID, _ = strconv.Atoi(first(p, "songid"))
	return st
}

// ParseSong converts a "currentsong" response. The first value of repeated
// tags wins; a missing title falls back to the file name.
func ParseSong(p []kv) Song {
	s := Song{
		File:   first(p, "file"),
		Title:  first(p, "Title"),
		Artist: first(p, "Artist", "AlbumArtist"),
		Album:  first(p, "Album"),
	}
	s.Duration, _ = strconv.ParseFloat(first(p, "duration"), 64)
	if s.Duration == 0 {
		s.Duration, _ = strconv.ParseFloat(first(p, "Time"), 64)
	}
	if s.Title == "" && s.File != "" {
		base := s.File
		if i := strings.LastIndexAny(base, `/\`); i >= 0 {
			base = base[i+1:]
		}
		if i := strings.LastIndex(base, "."); i > 0 {
			base = base[:i]
		}
		s.Title = base
	}
	return s
}

func quote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// Getenv is os.Getenv, exposed so callers need not import os just for Resolve.
func Getenv(k string) string { return os.Getenv(k) }
