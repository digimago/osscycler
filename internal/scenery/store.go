package scenery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/digimago/osscycler/internal/course"
)

// stretchM is how much of a course one fetch covers: a request small
// enough for a busy server, and the part the rider is on comes first.
const stretchM = 5000.0

// retryAfter spaces the attempts for a course whose map data won't come;
// after the last one the store gives up until the course is wanted again.
var retryAfter = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}

// Config sets up a Store.
type Config struct {
	// CacheDir keeps the raw map data, one file per stretch of a course.
	CacheDir string
	// Fetch allows asking the Overpass server; without it only the cache
	// is used.
	Fetch     bool
	Endpoint  string // default DefaultOverpassURL
	UserAgent string
	Log       *slog.Logger
}

// Store holds the scenery of each course: from the cache straight away,
// and, for the courses being ridden, fetched in the background a stretch
// at a time.
type Store struct {
	cfg    Config
	client *http.Client
	wake   chan struct{}

	mu      sync.Mutex
	byID    map[string]*Scenery
	courses map[string]*entry
	want    map[string]*want
	seq     int
}

// entry is a course and which of its stretches are cached.
type entry struct {
	c     *course.Course
	parts []stretch
	have  []bool
}

// want is a course the store fetches: the stretch to start from, and its
// failures so far.
type want struct {
	from     int
	seq      int // later wants go first
	failures int
	retryAt  time.Time
}

// stretch is a part of a course with its own query and cache file.
type stretch struct {
	query string
	path  string
}

// NewStore returns a store for the scenery of courses; Run brings it in.
// The world builder passes no courses and calls Load.
func NewStore(cfg Config, courses []*course.Course) *Store {
	if cfg.Endpoint == "" {
		cfg.Endpoint = DefaultOverpassURL
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	s := &Store{cfg: cfg, client: &http.Client{Timeout: 4 * time.Minute}, wake: make(chan struct{}, 1),
		byID: map[string]*Scenery{}, courses: map[string]*entry{}, want: map[string]*want{}}
	for _, c := range courses {
		if !c.Builtin { // osscycler's own test tracks lie where there is no map
			s.courses[c.ID] = s.entryFor(c)
		}
	}
	return s
}

// Get returns a course's scenery; nil until some of it is known. It may
// cover only some stretches while the rest is being fetched.
func (s *Store) Get(id string) *Scenery {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byID[id]
}

// Want asks for the map data of a course, starting with the stretch at
// fromM: the course is being ridden. It does nothing when fetching is off
// or the course is complete.
func (s *Store) Want(id string, fromM float64) {
	if !s.cfg.Fetch {
		return
	}
	s.mu.Lock()
	s.seq++
	w := s.want[id]
	if w == nil {
		w = &want{}
		s.want[id] = w
	}
	w.from, w.seq = max(0, int(fromM/stretchM)), s.seq
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Pending tells whether more of the course's map data is on its way: it
// is wanted and stretches are missing. Renderers then ask again.
func (s *Store) Pending(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.courses[id]
	return s.want[id] != nil && e != nil && e.next(0) >= 0
}

// next is the first missing stretch from `from` on, then before it; -1
// when the course is complete.
func (e *entry) next(from int) int {
	n := len(e.parts)
	for i := range n {
		k := (min(from, n-1) + i) % n
		if !e.have[k] {
			return k
		}
	}
	return -1
}

// Run loads the cached scenery of the courses, then fetches the
// stretches the wanted courses lack, one at a time to go easy on the
// shared server, retrying failures for a while. It returns when ctx ends.
func (s *Store) Run(ctx context.Context) {
	s.loadCache(ctx)
	for {
		id, k, wait := s.pick()
		if id == "" {
			var timer <-chan time.Time
			if wait > 0 {
				timer = time.After(wait)
			}
			select {
			case <-ctx.Done():
				return
			case <-s.wake:
			case <-timer:
			}
			continue
		}
		s.fetchOne(ctx, id, k)
		if ctx.Err() != nil {
			return
		}
	}
}

// loadCache builds the scenery of every course from its cached stretches.
func (s *Store) loadCache(ctx context.Context) {
	s.mu.Lock()
	es := make([]*entry, 0, len(s.courses))
	for _, e := range s.courses {
		es = append(es, e)
	}
	s.mu.Unlock()
	for _, e := range es {
		if ctx.Err() != nil {
			return
		}
		s.rebuild(e)
	}
}

// pick chooses the stretch to fetch next: from the latest wanted course
// that may try now. Otherwise it says how long until one may (0: none).
func (s *Store) pick() (id string, k int, wait time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now, best := time.Now(), -1
	for wid, w := range s.want {
		e := s.courses[wid]
		next := -1
		if e != nil {
			next = e.next(w.from)
		}
		if next < 0 {
			delete(s.want, wid) // complete, or a course without a map
			continue
		}
		if d := w.retryAt.Sub(now); d > 0 {
			if wait == 0 || d < wait {
				wait = d
			}
			continue
		}
		if w.seq > best {
			id, k, best = wid, next, w.seq
		}
	}
	return id, k, wait
}

func (s *Store) fetchOne(ctx context.Context, id string, k int) {
	s.mu.Lock()
	e := s.courses[id]
	s.mu.Unlock()
	start := time.Now()
	raw, err := s.fetchStretch(ctx, e, k)
	if ctx.Err() != nil {
		return
	}
	gaveUp := false
	s.mu.Lock()
	w := s.want[id]
	if err == nil {
		if w != nil {
			w.failures = 0
		}
	} else if w != nil {
		w.failures++
		if w.failures > len(retryAfter) {
			delete(s.want, id) // until it is wanted again
			gaveUp = true
		} else {
			w.retryAt = time.Now().Add(retryAfter[w.failures-1])
		}
	}
	s.mu.Unlock()
	if err != nil {
		s.cfg.Log.Warn("fetching map data failed", "course", id, "stretch", k, "server", s.cfg.Endpoint, "err", err)
		if gaveUp {
			s.cfg.Log.Warn("giving up on map data until the course is ridden again", "course", id)
		}
		return
	}
	s.cfg.Log.Info("map data fetched", "course", id, "stretch", k, "of", len(e.parts), "bytes", len(raw), "took", time.Since(start).Round(time.Millisecond))
	s.rebuild(e)
}

// fetchStretch fetches stretch k of e and caches it. Data that can't be
// cached counts as not fetched: it would only be fetched again and again.
func (s *Store) fetchStretch(ctx context.Context, e *entry, k int) ([]byte, error) {
	raw, err := s.fetch(ctx, e.parts[k].query)
	if err == nil {
		_, err = Parse(raw)
	}
	if err != nil {
		return nil, err
	}
	if err := s.save(e, k, raw); err != nil {
		return nil, fmt.Errorf("caching: %w", err)
	}
	return raw, nil
}

// rateLimitTries is how often a query is asked while the server says
// this client's slots are taken, waiting for one each time.
const rateLimitTries = 4

// maxSlotWait caps a wait for a slot.
var maxSlotWait = 2 * time.Minute

// fetch runs a query, waiting for a free slot when the server says all of
// this client's are taken.
func (s *Store) fetch(ctx context.Context, query string) ([]byte, error) {
	for try := 1; ; try++ {
		raw, err := Fetch(ctx, s.client, s.cfg.Endpoint, s.cfg.UserAgent, query)
		if !errors.Is(err, ErrRateLimited) || try == rateLimitTries {
			return raw, err
		}
		wait := slotWait(ctx, s.client, s.cfg.Endpoint, s.cfg.UserAgent, maxSlotWait)
		s.cfg.Log.Info("map data server busy for us; waiting for a slot", "wait", wait)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
}

// rebuild makes the course's scenery again from its cached stretches;
// the stretches count as there once the scenery has them.
func (s *Store) rebuild(e *entry) {
	d, have, err := s.readCached(e)
	for _, err := range err {
		s.cfg.Log.Warn("cached map data unreadable; it will be fetched again", "course", e.c.ID, "err", err)
	}
	var sc *Scenery
	if d != nil {
		sc = Build(e.c, d)
	}
	s.mu.Lock()
	copy(e.have, have)
	if sc != nil {
		s.byID[e.c.ID] = sc
	}
	s.mu.Unlock()
}

// readCached reads the course's cached stretches: their data merged (nil
// when there are none), which ones were there, and the files that
// couldn't be read.
func (s *Store) readCached(e *entry) (*Data, []bool, []error) {
	var parts []*Data
	var errs []error
	have := make([]bool, len(e.parts))
	for k, p := range e.parts {
		raw, err := os.ReadFile(p.path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		var d *Data
		if err == nil {
			d, err = Parse(raw)
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		parts, have[k] = append(parts, d), true
	}
	if len(parts) == 0 {
		return nil, have, errs
	}
	return merge(parts), have, errs
}

// Load returns c's map data for a program that needs all of it now (the
// world builder): the cached stretches, and the missing ones fetched one
// after the other when fetching is on. Stretches that couldn't be had are
// left out, and the error says which.
func (s *Store) Load(ctx context.Context, c *course.Course) (*Data, error) {
	return s.LoadProgress(ctx, c, nil)
}

// LoadProgress is Load telling progress how many stretches are there of
// how many as it goes (fetch: how many of those it had to fetch so far,
// and how many are left to fetch).
func (s *Store) LoadProgress(ctx context.Context, c *course.Course, progress func(done, total, toFetch int)) (*Data, error) {
	e := s.entryFor(c)
	d, have, errs := s.readCached(e)
	if !s.cfg.Fetch {
		for k, ok := range have {
			if !ok {
				errs = append(errs, fmt.Errorf("stretch %d: not cached, and fetching is off", k))
			}
		}
		return orEmpty(d), errors.Join(errs...)
	}
	fetched := false
	done, toFetch := 0, 0
	for _, ok := range have {
		if ok {
			done++
		} else {
			toFetch++
		}
	}
	if progress != nil {
		progress(done, len(have), toFetch)
	}
	for k, ok := range have {
		if ok {
			continue
		}
		_, err := s.fetchStretch(ctx, e, k)
		done++
		toFetch--
		if progress != nil {
			progress(done, len(have), toFetch)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("stretch %d: %w", k, err))
			continue
		}
		fetched = true
	}
	if fetched {
		d, _, _ = s.readCached(e)
	}
	return orEmpty(d), errors.Join(errs...)
}

// Missing is how many of c's stretches aren't cached (to be fetched):
// for a build's estimate of the time left.
func (s *Store) Missing(c *course.Course) int {
	n := 0
	for _, p := range s.entryFor(c).parts {
		if _, err := os.Stat(p.path); err != nil {
			n++
		}
	}
	return n
}

func orEmpty(d *Data) *Data {
	if d == nil {
		return &Data{}
	}
	return d
}

// entryFor splits c into its stretches; the last one takes a short rest.
func (s *Store) entryFor(c *course.Course) *entry {
	n := max(1, int(math.Round(c.Distance/stretchM)))
	bounds := make([]float64, n+1)
	for k := range n {
		bounds[k] = float64(k) * stretchM
	}
	bounds[n] = c.Distance
	e := &entry{c: c, have: make([]bool, n)}
	for k, q := range queries(c, bounds) {
		e.parts = append(e.parts, stretch{q, s.cachePath(c.ID, k, q)})
	}
	return e
}

// cachePath names the cache file after the course, the stretch and its
// query, so an edited GPX fetches afresh.
func (s *Store) cachePath(id string, k int, query string) string {
	sum := sha256.Sum256([]byte(query))
	return filepath.Join(s.cfg.CacheDir, fmt.Sprintf("%s-%d-%s.json", id, k, hex.EncodeToString(sum[:6])))
}

// stretchFile is the rest of a cache file's name after the course ID: the
// stretch and the query hash. oldFile is a whole course in one file, as
// before stretches.
var (
	stretchFile = regexp.MustCompile(`^([0-9]+)-[0-9a-f]{12}\.json$`)
	oldFile     = regexp.MustCompile(`^[0-9a-f]{12}\.json$`)
)

// save writes stretch k's map data atomically and removes the files it
// replaces: the stretch's older ones, stretches the course no longer has,
// and a whole-course file from before stretches.
func (s *Store) save(e *entry, k int, raw []byte) error {
	path := e.parts[k].path
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
	current := map[string]bool{}
	for _, p := range e.parts {
		current[filepath.Base(p.path)] = true
	}
	entries, _ := os.ReadDir(s.cfg.CacheDir)
	for _, f := range entries {
		name := f.Name()
		rest, ok := strings.CutPrefix(name, e.c.ID+"-")
		if !ok || current[name] {
			continue
		}
		stale := oldFile.MatchString(rest)
		if m := stretchFile.FindStringSubmatch(rest); m != nil {
			j, err := strconv.Atoi(m[1])
			stale = err != nil || j == k || j >= len(e.parts)
		}
		if stale {
			os.Remove(filepath.Join(s.cfg.CacheDir, name))
		}
	}
	return nil
}
