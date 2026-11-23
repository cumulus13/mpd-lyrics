// Package lrclib is a small client for the LRCLIB lyrics API
// (https://lrclib.net). Lyrics are always looked up dynamically for the
// track that is playing; results are cached on disk to be polite to the API.
package lrclib

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrNotFound is returned when LRCLIB has no usable lyrics for the track.
var ErrNotFound = errors.New("lrclib: lyrics not found")

// Track identifies what is playing.
type Track struct {
	Title    string
	Artist   string
	Album    string
	Duration time.Duration
}

// Result mirrors the fields of an LRCLIB record we care about.
type Result struct {
	ID           int     `json:"id"`
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	AlbumName    string  `json:"albumName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	PlainLyrics  string  `json:"plainLyrics"`
	SyncedLyrics string  `json:"syncedLyrics"`
}

// HasLyrics reports whether the record carries any lyric text.
func (r *Result) HasLyrics() bool { return r.SyncedLyrics != "" || r.PlainLyrics != "" }

// Client talks to an LRCLIB server.
type Client struct {
	BaseURL     string
	UserAgent   string
	HTTP        *http.Client
	Cache       *Cache // optional
	TTL         time.Duration
	NotFoundTTL time.Duration
	MaxDurDiff  time.Duration // 0 disables the duration sanity check
}

// Lookup finds lyrics for tr. When refresh is true the cache is bypassed for
// reading (the fresh answer is still stored).
func (c *Client) Lookup(ctx context.Context, tr Track, refresh bool) (*Result, error) {
	if strings.TrimSpace(tr.Title) == "" {
		return nil, ErrNotFound
	}
	key := cacheKey(tr)
	if c.Cache != nil && !refresh {
		if e, ok := c.Cache.get(key); ok {
			ttl := c.TTL
			if e.NotFound {
				ttl = c.NotFoundTTL
			}
			if time.Since(e.FetchedAt) < ttl {
				if e.NotFound || e.Result == nil {
					return nil, ErrNotFound
				}
				return e.Result, nil
			}
		}
	}

	res, err := c.fetch(ctx, tr)
	switch {
	case err == nil:
		if c.Cache != nil {
			c.Cache.put(key, entry{FetchedAt: time.Now(), Result: res})
		}
		return res, nil
	case errors.Is(err, ErrNotFound):
		if c.Cache != nil {
			c.Cache.put(key, entry{FetchedAt: time.Now(), NotFound: true})
		}
		return nil, err
	}
	return nil, err // transient errors are never cached
}

func (c *Client) fetch(ctx context.Context, tr Track) (*Result, error) {
	titles := []string{tr.Title}
	if clean := CleanTitle(tr.Title); clean != "" && clean != tr.Title {
		titles = append(titles, clean)
	}

	// 1. Exact signature match (needs album and duration).
	if tr.Album != "" && tr.Duration > 0 && tr.Artist != "" {
		for _, title := range titles {
			q := url.Values{}
			q.Set("track_name", title)
			q.Set("artist_name", tr.Artist)
			q.Set("album_name", tr.Album)
			q.Set("duration", strconv.Itoa(int(math.Round(tr.Duration.Seconds()))))
			var r Result
			status, err := c.getJSON(ctx, "/api/get", q, &r)
			if err != nil {
				return nil, err
			}
			if status == http.StatusOK && (r.HasLyrics() || r.Instrumental) {
				return &r, nil
			}
		}
	}

	// 2. Fuzzy search.
	for _, title := range titles {
		for _, q := range searchQueries(title, tr.Artist) {
			var rs []Result
			status, err := c.getJSON(ctx, "/api/search", q, &rs)
			if err != nil {
				return nil, err
			}
			if status != http.StatusOK {
				continue
			}
			if best := pick(rs, tr, c.MaxDurDiff); best != nil {
				return best, nil
			}
		}
	}
	return nil, ErrNotFound
}

func searchQueries(title, artist string) []url.Values {
	var out []url.Values
	q := url.Values{}
	q.Set("track_name", title)
	if artist != "" {
		q.Set("artist_name", artist)
	}
	out = append(out, q)
	if artist != "" {
		q2 := url.Values{}
		q2.Set("q", artist+" "+title)
		out = append(out, q2)
	}
	return out
}

func (c *Client) getJSON(ctx context.Context, path string, q url.Values, out any) (int, error) {
	u := strings.TrimRight(c.BaseURL, "/") + path + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	ua := c.UserAgent
	if ua == "" {
		ua = "mpd-lyrics"
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Lrclib-Client", ua)
	req.Header.Set("Accept", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, fmt.Errorf("lrclib: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return resp.StatusCode, fmt.Errorf("lrclib: reading response: %w", err)
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return resp.StatusCode, nil
	case resp.StatusCode != http.StatusOK:
		return resp.StatusCode, fmt.Errorf("lrclib: HTTP %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return resp.StatusCode, fmt.Errorf("lrclib: bad JSON: %w", err)
	}
	return resp.StatusCode, nil
}

// pick chooses the best candidate: synced lyrics first, then closest duration
// and matching names. Candidates whose duration is too far off are rejected.
func pick(rs []Result, tr Track, maxDiff time.Duration) *Result {
	bestIdx, bestScore := -1, math.Inf(-1)
	for i := range rs {
		r := &rs[i]
		if !r.HasLyrics() && !r.Instrumental {
			continue
		}
		score := 0.0
		if r.SyncedLyrics != "" {
			score += 100
		}
		if tr.Duration > 0 && r.Duration > 0 {
			diff := time.Duration(math.Abs(r.Duration-tr.Duration.Seconds()) * float64(time.Second))
			if maxDiff > 0 && diff > maxDiff {
				continue
			}
			switch {
			case diff <= 2*time.Second:
				score += 50
			case diff <= 5*time.Second:
				score += 20
			default:
				score -= diff.Seconds()
			}
		}
		if strings.EqualFold(r.ArtistName, tr.Artist) {
			score += 10
		}
		if strings.EqualFold(r.TrackName, tr.Title) {
			score += 10
		}
		if tr.Album != "" && strings.EqualFold(r.AlbumName, tr.Album) {
			score += 5
		}
		if score > bestScore {
			bestIdx, bestScore = i, score
		}
	}
	if bestIdx < 0 {
		return nil
	}
	return &rs[bestIdx]
}

var (
	bracketNoise = regexp.MustCompile(`(?i)\s*[\(\[\{][^\)\]\}]*?\b(feat|ft|featuring|remaster|remastered|live|version|edit|mono|stereo|official|video|audio|lyric|lyrics|explicit|deluxe|bonus)\b[^\)\]\}]*[\)\]\}]`)
	dashNoise    = regexp.MustCompile(`(?i)\s+[-–—]\s+(\d{4}\s+)?(remaster(ed)?|live|mono|stereo|single version|radio edit|.*\bversion)\b.*$`)
)

// CleanTitle strips decorations such as "(feat. X)", "[Official Video]" and
// "- 2011 Remaster" that commonly prevent an exact match.
func CleanTitle(s string) string {
	s = bracketNoise.ReplaceAllString(s, "")
	s = dashNoise.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}
