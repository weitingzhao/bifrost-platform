// Package threadtitles gives agent threads human names for the lineage views.
//
// Claude Code keeps each session's title (the one the user set, or the one it
// generated) inside the session transcript as "custom-title" / "ai-title"
// records. Commits carry the transcript id (Claude-Transcript trailer), so a
// transcript id -> title map names every thread. The transcripts live only on
// the workstation that ran the sessions, so each session's Stop hook reports its
// own title to a ConfigMap the in-cluster API reads. Names set by hand (manual)
// win over reported ones.
package threadtitles

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
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

var transcriptID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// ReportTranscript records the title a session reported for its own transcript
// (Claude Code Stop hook). The same title again writes nothing.
func (s *Store) ReportTranscript(ctx context.Context, transcript, title string, now time.Time) error {
	transcript = strings.TrimSpace(transcript)
	title = strings.TrimSpace(title)
	if !transcriptID.MatchString(transcript) {
		return fmt.Errorf("transcript must be a session uuid")
	}
	if title == "" {
		return fmt.Errorf("title is required")
	}
	if len([]rune(title)) > maxTitle {
		return fmt.Errorf("title longer than %d characters", maxTitle)
	}
	return s.update(ctx, func(t *Titles) bool {
		if cur, ok := t.Transcripts[transcript]; ok && cur.Title == title {
			return false
		}
		t.Transcripts[transcript] = Entry{Title: title, At: now}
		return true
	})
}
