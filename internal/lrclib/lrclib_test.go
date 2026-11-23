package lrclib

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type fakeServer struct {
	*httptest.Server
	gets, searches atomic.Int32
}

func newFake(t *testing.T, get func(q map[string]string) (int, any), search func(q map[string]string) []Result) *fakeServer {
	t.Helper()
	f := &fakeServer{}
	mux := http.NewServeMux()
	flat := func(r *http.Request) map[string]string {
		m := map[string]string{}
		for k, v := range r.URL.Query() {
			m[k] = v[0]
		}
		return m
	}
	mux.HandleFunc("/api/get", func(w http.ResponseWriter, r *http.Request) {
		f.gets.Add(1)
		if r.Header.Get("Lrclib-Client") == "" || r.Header.Get("User-Agent") == "" {
			t.Error("missing client headers")
		}
		code, body := get(flat(r))
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(body)
	})
	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		f.searches.Add(1)
		_ = json.NewEncoder(w).Encode(search(flat(r)))
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func client(url string, cache *Cache) *Client {
	return &Client{BaseURL: url, UserAgent: "test/1", Cache: cache, TTL: time.Hour, NotFoundTTL: time.Hour, MaxDurDiff: 8 * time.Second}
}

var track = Track{Title: "Song", Artist: "Band", Album: "LP", Duration: 200 * time.Second}

func TestLookupExactGet(t *testing.T) {
	f := newFake(t,
		func(q map[string]string) (int, any) {
			if q["duration"] != "200" || q["album_name"] != "LP" {
				t.Errorf("bad query %v", q)
			}
			return 200, Result{ID: 1, TrackName: "Song", SyncedLyrics: "[00:01.00]hi"}
		},
		func(map[string]string) []Result { t.Error("search must not be called"); return nil })
	r, err := client(f.URL, nil).Lookup(context.Background(), track, false)
	if err != nil || r.ID != 1 {
		t.Fatalf("r=%+v err=%v", r, err)
	}
}

func TestLookupFallsBackToSearchAndPicksBest(t *testing.T) {
	f := newFake(t,
		func(map[string]string) (int, any) { return 404, map[string]any{"code": 404} },
		func(map[string]string) []Result {
			return []Result{
				{ID: 1, Duration: 300, SyncedLyrics: "[00:01.00]wrong length"},
				{ID: 2, Duration: 200.4, PlainLyrics: "plain only"},
				{ID: 3, Duration: 201, SyncedLyrics: "[00:01.00]good", ArtistName: "band", TrackName: "song"},
			}
		})
	r, err := client(f.URL, nil).Lookup(context.Background(), track, false)
	if err != nil || r.ID != 3 {
		t.Fatalf("r=%+v err=%v", r, err)
	}
}

func TestLookupRejectsWrongDuration(t *testing.T) {
	f := newFake(t,
		func(map[string]string) (int, any) { return 404, nil },
		func(map[string]string) []Result { return []Result{{ID: 1, Duration: 400, SyncedLyrics: "[00:01.00]x"}} })
	if _, err := client(f.URL, nil).Lookup(context.Background(), track, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestLookupWithoutAlbumSkipsGet(t *testing.T) {
	f := newFake(t,
		func(map[string]string) (int, any) { t.Error("get must be skipped"); return 404, nil },
		func(map[string]string) []Result { return []Result{{ID: 9, Duration: 200, SyncedLyrics: "[00:01.00]x"}} })
	tr := track
	tr.Album = ""
	r, err := client(f.URL, nil).Lookup(context.Background(), tr, false)
	if err != nil || r.ID != 9 {
		t.Fatalf("r=%+v err=%v", r, err)
	}
}

func TestCacheHitAndNegativeCache(t *testing.T) {
	cache := &Cache{Dir: t.TempDir()}
	f := newFake(t,
		func(map[string]string) (int, any) { return 200, Result{ID: 5, SyncedLyrics: "[00:01.00]x"} },
		func(map[string]string) []Result { return nil })
	c := client(f.URL, cache)
	for i := 0; i < 3; i++ {
		if r, err := c.Lookup(context.Background(), track, false); err != nil || r.ID != 5 {
			t.Fatalf("lookup %d: %+v %v", i, r, err)
		}
	}
	if f.gets.Load() != 1 {
		t.Errorf("server hit %d times, want 1 (cache)", f.gets.Load())
	}
	if _, err := c.Lookup(context.Background(), track, true); err != nil {
		t.Fatal(err)
	}
	if f.gets.Load() != 2 {
		t.Errorf("refresh should bypass cache, hits=%d", f.gets.Load())
	}

	// negative cache
	f2 := newFake(t,
		func(map[string]string) (int, any) { return 404, nil },
		func(map[string]string) []Result { return nil })
	c2 := client(f2.URL, &Cache{Dir: t.TempDir()})
	for i := 0; i < 2; i++ {
		if _, err := c2.Lookup(context.Background(), track, false); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	}
	before := f2.gets.Load() + f2.searches.Load()
	if _, err := c2.Lookup(context.Background(), track, false); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if f2.gets.Load()+f2.searches.Load() != before {
		t.Error("negative result was not cached")
	}
}

func TestTransientErrorsAreNotCached(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/get", func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			http.Error(w, "boom", 500)
			return
		}
		_ = json.NewEncoder(w).Encode(Result{ID: 7, SyncedLyrics: "[00:01.00]x"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client(srv.URL, &Cache{Dir: t.TempDir()})
	if _, err := c.Lookup(context.Background(), track, false); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("expected transient error, got %v", err)
	}
	fail.Store(false)
	if r, err := c.Lookup(context.Background(), track, false); err != nil || r.ID != 7 {
		t.Fatalf("after recovery: %+v %v", r, err)
	}
}

func TestCleanTitle(t *testing.T) {
	cases := map[string]string{
		"Song (feat. Someone)":    "Song",
		"Song [Official Video]":   "Song",
		"Song - 2011 Remaster":    "Song",
		"Song - Radio Edit":       "Song",
		"Alive":                   "Alive",
		"Hello (Part 2)":          "Hello (Part 2)",
		"Track - Live at Wembley": "Track",
		"Plain":                   "Plain",
	}
	for in, want := range cases {
		if got := CleanTitle(in); got != want {
			t.Errorf("CleanTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEmptyTitle(t *testing.T) {
	if _, err := (&Client{}).Lookup(context.Background(), Track{}, false); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
