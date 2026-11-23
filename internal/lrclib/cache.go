package lrclib

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Cache is a tiny on-disk cache: one JSON file per track.
type Cache struct {
	Dir string
}

type entry struct {
	FetchedAt time.Time `json:"fetched_at"`
	NotFound  bool      `json:"not_found,omitempty"`
	Result    *Result   `json:"result,omitempty"`
}

func cacheKey(tr Track) string {
	raw := strings.ToLower(strings.Join([]string{
		strings.TrimSpace(tr.Artist),
		strings.TrimSpace(tr.Title),
		strings.TrimSpace(tr.Album),
		itoa(int(math.Round(tr.Duration.Seconds()))),
	}, "\x00"))
	sum := sha1.Sum([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func (c *Cache) path(key string) string { return filepath.Join(c.Dir, key+".json") }

func (c *Cache) get(key string) (entry, bool) {
	var e entry
	b, err := os.ReadFile(c.path(key))
	if err != nil {
		return e, false
	}
	if json.Unmarshal(b, &e) != nil {
		return e, false
	}
	return e, true
}

// put writes atomically; failures are ignored because the cache is only an
// optimisation.
func (c *Cache) put(key string, e entry) {
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return
	}
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(c.Dir, "tmp-*")
	if err != nil {
		return
	}
	name := tmp.Name()
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(name, c.path(key)) != nil {
		os.Remove(name)
	}
}
