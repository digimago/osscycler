package scenery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/digimago/osscycler/course"
)

// retryAfter spaces the attempts for a course whose map data won't come;
// after the last one the core gives up until its next start.
var retryAfter = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}

// Config sets up a Store.
type Config struct {
	// CacheDir keeps the raw map data, one file per course and query.
	CacheDir string
	// Fetch allows asking the Overpass server; without it only the cache
	// is used.
	Fetch     bool
	Endpoint  string // default DefaultOverpassURL
	UserAgent string
	Log       *slog.Logger
}

// Store holds the scenery of each course: from the cache straight away,
// fetched in the background otherwise.
type Store struct {
	cfg    Config
	client *http.Client

	mu   sync.Mutex
	byID map[string]*Scenery
}

func NewStore(cfg Config) *Store {
	if cfg.Endpoint == "" {
		cfg.Endpoint = DefaultOverpassURL
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	return &Store{cfg: cfg, client: &http.Client{Timeout: 4 * time.Minute}, byID: map[string]*Scenery{}}
}

// Get returns a course's scenery; nil until it is known.
func (s *Store) Get(id string) *Scenery {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byID[id]
}

func (s *Store) set(id string, sc *Scenery) {
	s.mu.Lock()
	s.byID[id] = sc
	s.mu.Unlock()
}

// Run loads the cached scenery of courses, then fetches what is missing,
// one course at a time to go easy on the shared server, retrying failures
// for a while. It returns when every course has scenery, it gives up, or
// ctx ends.
func (s *Store) Run(ctx context.Context, courses []*course.Course) {
	type job struct {
		c     *course.Course
		query string
		path  string
	}
	var pending []job
	for _, c := range courses {
		q := Query(c)
		j := job{c, q, s.cachePath(c.ID, q)}
		if raw, err := os.ReadFile(j.path); err == nil {
			if err := s.build(c, raw); err == nil {
				continue
			}
			s.cfg.Log.Warn("cached map data unreadable; fetching again", "course", c.ID, "err", err)
		}
		pending = append(pending, j)
	}
	if len(pending) == 0 {
		return
	}
	if !s.cfg.Fetch {
		s.cfg.Log.Info("no map data for some courses, and fetching it is off", "courses", len(pending))
		return
	}
	for attempt := 0; ; attempt++ {
		var failed []job
		for _, j := range pending {
			if ctx.Err() != nil {
				return
			}
			start := time.Now()
			raw, err := Fetch(ctx, s.client, s.cfg.Endpoint, s.cfg.UserAgent, j.query)
			if err == nil {
				err = s.build(j.c, raw)
			}
			if err != nil {
				s.cfg.Log.Warn("fetching map data failed", "course", j.c.ID, "server", s.cfg.Endpoint, "err", err)
				failed = append(failed, j)
				continue
			}
			s.cfg.Log.Info("map data fetched", "course", j.c.ID, "bytes", len(raw), "took", time.Since(start).Round(time.Millisecond))
			if err := s.save(j.c.ID, j.path, raw); err != nil {
				s.cfg.Log.Warn("caching map data failed", "course", j.c.ID, "err", err)
			}
		}
		pending = failed
		if len(pending) == 0 {
			return
		}
		if attempt >= len(retryAfter) {
			s.cfg.Log.Warn("giving up on map data until the next start", "courses", len(pending))
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(retryAfter[attempt]):
		}
	}
}

func (s *Store) build(c *course.Course, raw []byte) error {
	d, err := Parse(raw)
	if err != nil {
		return err
	}
	s.set(c.ID, Build(c, d))
	return nil
}

// cachePath names the cache file after the course and its query, so an
// edited GPX fetches afresh.
func (s *Store) cachePath(id, query string) string {
	sum := sha256.Sum256([]byte(query))
	return filepath.Join(s.cfg.CacheDir, id+"-"+hex.EncodeToString(sum[:6])+".json")
}

// save writes the map data atomically and removes the course's older
// files.
func (s *Store) save(id, path string, raw []byte) error {
	if err := os.MkdirAll(s.cfg.CacheDir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.cfg.CacheDir, ".fetch-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	entries, _ := os.ReadDir(s.cfg.CacheDir)
	for _, e := range entries {
		name := e.Name()
		rest, ok := strings.CutPrefix(name, id+"-")
		if ok && name != filepath.Base(path) && len(rest) == len("0123456789ab.json") && strings.HasSuffix(rest, ".json") {
			os.Remove(filepath.Join(s.cfg.CacheDir, name))
		}
	}
	return nil
}
