// Package threadtitles gives agent threads human names for the lineage views.
//
// Claude Code keeps each session's title (the one the user set, or the one it
// generated) inside the session transcript as "custom-title" / "ai-title"
// records. Commits carry the transcript id (Claude-Transcript trailer), so a
// transcript id -> title map names every thread. The transcripts live only on
// the workstation that ran the sessions, so a syncer there reads the titles —
// nothing else from the transcripts — and writes them to a ConfigMap the
// in-cluster API reads. Names set by hand (manual) win over synced ones.
package threadtitles

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

const (
	configMapName = "lineage-thread-titles"
	keyAuto       = "transcripts.json"
	keyManual     = "manual.json"
	maxTitle      = 120
)

// Entry is one title and when it was last seen or set.
type Entry struct {
	Title string    `json:"title"`
	At    time.Time `json:"at"`
}

// Titles: Transcripts is transcript id -> title (synced); Manual is session -> title (set by hand).
type Titles struct {
	Transcripts map[string]Entry `json:"transcripts"`
	Manual      map[string]Entry `json:"manual"`
}

// Store keeps the titles in one ConfigMap.
type Store struct {
	clients func() (kubernetes.Interface, error)
	ns      string
}

func NewStore(ns string, clients func() (kubernetes.Interface, error)) *Store {
	if ns == "" {
		ns = "cicd"
	}
	return &Store{clients: clients, ns: ns}
}

func decode(cm *corev1.ConfigMap) Titles {
	t := Titles{Transcripts: map[string]Entry{}, Manual: map[string]Entry{}}
	if cm == nil {
		return t
	}
	_ = json.Unmarshal([]byte(cm.Data[keyAuto]), &t.Transcripts)
	_ = json.Unmarshal([]byte(cm.Data[keyManual]), &t.Manual)
	if t.Transcripts == nil {
		t.Transcripts = map[string]Entry{}
	}
	if t.Manual == nil {
		t.Manual = map[string]Entry{}
	}
	return t
}

// Load returns the stored titles; a missing ConfigMap is empty, not an error.
func (s *Store) Load(ctx context.Context) (Titles, error) {
	core, err := s.clients()
	if err != nil {
		return decode(nil), err
	}
	cm, err := core.CoreV1().ConfigMaps(s.ns).Get(ctx, configMapName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return decode(nil), nil
	}
	if err != nil {
		return decode(nil), err
	}
	return decode(cm), nil
}

// update reads, edits and writes the ConfigMap, creating it when missing; it
// retries on a write conflict. edit returns false when nothing changed.
func (s *Store) update(ctx context.Context, edit func(*Titles) bool) error {
	core, err := s.clients()
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 3; attempt++ {
		cm, err := core.CoreV1().ConfigMaps(s.ns).Get(ctx, configMapName, metav1.GetOptions{})
		missing := apierrors.IsNotFound(err)
		if err != nil && !missing {
			return err
		}
		t := decode(cm)
		if !edit(&t) {
			return nil
		}
		auto, _ := json.Marshal(t.Transcripts)
		manual, _ := json.Marshal(t.Manual)
		if missing {
			cm = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: s.ns,
				Labels: map[string]string{"app.kubernetes.io/managed-by": "bifrost-platform"}}}
		}
		cm.Data = map[string]string{keyAuto: string(auto), keyManual: string(manual)}
		if missing {
			_, err = core.CoreV1().ConfigMaps(s.ns).Create(ctx, cm, metav1.CreateOptions{})
		} else {
			_, err = core.CoreV1().ConfigMaps(s.ns).Update(ctx, cm, metav1.UpdateOptions{})
		}
		if apierrors.IsConflict(err) || apierrors.IsAlreadyExists(err) {
			continue
		}
		return err
	}
	return fmt.Errorf("thread titles: still conflicting after 3 attempts")
}

// SetManual names a thread by hand; an empty title removes the hand-set name.
func (s *Store) SetManual(ctx context.Context, session, title string, now time.Time) error {
	session = strings.TrimSpace(session)
	title = strings.TrimSpace(title)
	if session == "" {
		return fmt.Errorf("session is required")
	}
	if len([]rune(title)) > maxTitle {
		return fmt.Errorf("title longer than %d characters", maxTitle)
	}
	return s.update(ctx, func(t *Titles) bool {
		if title == "" {
			if _, ok := t.Manual[session]; !ok {
				return false
			}
			delete(t.Manual, session)
			return true
		}
		if t.Manual[session].Title == title {
			return false
		}
		t.Manual[session] = Entry{Title: title, At: now}
		return true
	})
}

// MergeTranscripts writes synced titles that are new or changed; it never
// deletes (a transcript cleaned off the workstation keeps its last title).
func (s *Store) MergeTranscripts(ctx context.Context, seen map[string]Entry) (int, error) {
	changed := 0
	err := s.update(ctx, func(t *Titles) bool {
		changed = 0
		for id, e := range seen {
			if cur, ok := t.Transcripts[id]; !ok || cur.Title != e.Title {
				t.Transcripts[id] = e
				changed++
			}
		}
		return changed > 0
	})
	return changed, err
}

// --- workstation syncer ---

// SyncWanted: PLATFORM_THREAD_TITLE_SYNC=on|off overrides; otherwise sync only
// outside the cluster (the transcripts are on the workstation) when the
// transcript directory exists.
func SyncWanted(dir string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PLATFORM_THREAD_TITLE_SYNC"))) {
	case "on", "true", "1":
		return true
	case "off", "false", "0":
		return false
	}
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		return false
	}
	st, err := os.Stat(dir)
	return err == nil && st.IsDir()
}

// TranscriptDir is CLAUDE_PROJECTS_DIR or ~/.claude/projects.
func TranscriptDir() string {
	if d := strings.TrimSpace(os.Getenv("CLAUDE_PROJECTS_DIR")); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}

type fileState struct {
	mod   time.Time
	size  int64
	title string
}

// Scanner reads titles out of transcript files, rereading a file only when it changed.
type Scanner struct {
	dir    string
	maxAge time.Duration
	mu     sync.Mutex
	files  map[string]fileState
}

func NewScanner(dir string, maxAge time.Duration) *Scanner {
	return &Scanner{dir: dir, maxAge: maxAge, files: map[string]fileState{}}
}

var (
	markCustom = []byte(`"type":"custom-title"`)
	markAI     = []byte(`"type":"ai-title"`)
)

// titleOf returns the last custom title in a transcript, else its last generated one.
func titleOf(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	var custom, ai string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		isCustom := bytes.Contains(line, markCustom)
		if !isCustom && !bytes.Contains(line, markAI) {
			continue
		}
		var rec struct {
			CustomTitle string `json:"customTitle"`
			AITitle     string `json:"aiTitle"`
		}
		if json.Unmarshal(line, &rec) != nil {
			continue
		}
		if isCustom && rec.CustomTitle != "" {
			custom = rec.CustomTitle
		} else if rec.AITitle != "" {
			ai = rec.AITitle
		}
	}
	if custom != "" {
		return custom, sc.Err()
	}
	return ai, sc.Err()
}

// Scan returns transcript id -> title for transcripts modified within maxAge.
// Transcript ids are the file names (<uuid>.jsonl) directly under each project dir.
func (s *Scanner) Scan(now time.Time) (map[string]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	paths, err := filepath.Glob(filepath.Join(s.dir, "*", "*.jsonl"))
	if err != nil {
		return nil, err
	}
	out := map[string]Entry{}
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil || now.Sub(st.ModTime()) > s.maxAge {
			continue
		}
		prev, ok := s.files[p]
		if !ok || !prev.mod.Equal(st.ModTime()) || prev.size != st.Size() {
			title, err := titleOf(p)
			if err != nil {
				slog.Warn("thread title scan", "path", p, "err", err)
				continue
			}
			prev = fileState{mod: st.ModTime(), size: st.Size(), title: title}
			s.files[p] = prev
		}
		if prev.title != "" {
			out[strings.TrimSuffix(filepath.Base(p), ".jsonl")] = Entry{Title: prev.title, At: prev.mod.UTC()}
		}
	}
	return out, nil
}

// StartSync scans and merges every interval until ctx ends; the first pass logs.
func StartSync(ctx context.Context, store *Store, sc *Scanner, interval time.Duration) {
	safego.Go("threadtitles.sync", func() {
		first := true
		pass := func() {
			seen, err := sc.Scan(time.Now())
			if err == nil {
				var n int
				n, err = store.MergeTranscripts(ctx, seen)
				if first || n > 0 {
					slog.Info("thread titles sync", "first", first, "transcripts", len(seen), "changed", n)
				}
			}
			if err != nil {
				slog.Warn("thread titles sync", "err", err)
			}
			first = false
		}
		safego.Do("threadtitles.sync.tick", pass)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				safego.Do("threadtitles.sync.tick", pass)
			}
		}
	})
}
