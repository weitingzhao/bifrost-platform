package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	kt "k8s.io/client-go/testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

var queueNow = time.Date(2026, 11, 14, 22, 0, 0, 0, time.UTC)

// casConfigMaps gives the fake the API server's optimistic concurrency.
func casConfigMaps(cs *fake.Clientset) {
	var mu sync.Mutex
	gvr := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	cs.PrependReactor("update", "configmaps", func(a kt.Action) (bool, runtime.Object, error) {
		mu.Lock()
		defer mu.Unlock()
		next := a.(kt.UpdateAction).GetObject().(*corev1.ConfigMap).DeepCopy()
		old, err := cs.Tracker().Get(gvr, a.GetNamespace(), next.Name)
		if err != nil {
			return true, nil, err
		}
		if next.ResourceVersion != old.(*corev1.ConfigMap).ResourceVersion {
			return true, nil, apierrors.NewConflict(gvr.GroupResource(), next.Name, fmt.Errorf("stale"))
		}
		next.ResourceVersion += "x"
		return true, next, cs.Tracker().Update(gvr, next, a.GetNamespace())
	})
}

func queueHandler(t *testing.T) (*Handler, *fake.Clientset) {
	t.Helper()
	cs := fake.NewSimpleClientset()
	casConfigMaps(cs)
	s := NewService(&config.ClusterEntry{ID: "test"})
	s.SetClientsetForTest(cs)
	return NewHandlerForTest(s, nil), cs
}

func hold(t *testing.T, cs *fake.Clientset, who string, now time.Time) {
	t.Helper()
	if _, err := putReleaseWindow(context.Background(), cs, "cicd", windowInput{Who: who, What: "bifrost-platform", TTLMinutes: 5}, now); err != nil {
		t.Fatalf("hold %s: %v", who, err)
	}
}

func queueUp(t *testing.T, cs *fake.Clientset, who, thread string, minutes int, now time.Time) *WindowQueued {
	t.Helper()
	_, err := putReleaseWindow(context.Background(), cs, "cicd", windowInput{Who: who, What: "bifrost-platform", Queue: true, Thread: thread, Resume: "rerun " + who, QueueMinutes: minutes}, now)
	q, ok := err.(*WindowQueued)
	if !ok {
		t.Fatalf("queue %s: %v", who, err)
	}
	return q
}

func rawWindow(t *testing.T, cs *fake.Clientset) windowRecord {
	t.Helper()
	cm, err := cs.CoreV1().ConfigMaps("cicd").Get(context.Background(), releaseWindowConfigMap, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r, ok := parseWindowCM(cm)
	if !ok {
		t.Fatal("unreadable window")
	}
	return r
}

func advance(t *testing.T, cs *fake.Clientset, now time.Time) []windowNotice {
	t.Helper()
	n, err := advanceWindowQueue(context.Background(), cs, "cicd", now, loadWindowQueueConfig())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func topics(ns []windowNotice) string {
	var out []string
	for _, n := range ns {
		out = append(out, n.Topic+">"+n.Entry.Who)
	}
	return strings.Join(out, ",")
}

// 2026-11-14 card #557 was approved while the window was held and failed (release window held); queue instead
func TestPutQueuesInsteadOfFailing557(t *testing.T) {
	h, cs := queueHandler(t)
	hold(t, cs, "seat-a", time.Now().UTC())
	put := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.HandlePutReleaseWindow(rec, httptest.NewRequest(http.MethodPut, "/delivery/release-window", bytes.NewBufferString(body)))
		return rec
	}
	if got := put(`{"who":"seat-b","what":"bifrost-platform"}`); got.Code != http.StatusConflict {
		t.Fatalf("plain PUT against a holder: %d %s", got.Code, got.Body)
	}
	got := put(`{"who":"seat-b","what":"bifrost-platform","queue":true,"thread":"thread:local_b","resume":"deliver platform 04a14374 to STG"}`)
	var out struct {
		Position int            `json:"position"`
		Holder   map[string]any `json:"holder"`
		Queue    []queueView    `json:"queue"`
	}
	if got.Code != http.StatusAccepted || json.Unmarshal(got.Body.Bytes(), &out) != nil || out.Position != 1 || out.Holder["who"] != "seat-a" || len(out.Queue) != 1 {
		t.Fatalf("queue PUT: %d %s", got.Code, got.Body)
	}
	if bad := put(`{"who":"seat-c","what":"bifrost-platform","queue":true,"resume":"two\nlines"}`); bad.Code != http.StatusBadRequest {
		t.Fatalf("resume with a newline accepted: %d", bad.Code)
	}
	rec := httptest.NewRecorder()
	h.HandleGetReleaseWindow(rec, httptest.NewRequest(http.MethodGet, "/delivery/release-window", nil))
	if !strings.Contains(rec.Body.String(), `"queue":[{"position":1,"who":"seat-b"`) || !strings.Contains(rec.Body.String(), `"held_seconds"`) {
		t.Fatalf("GET lacks holder/queue: %s", rec.Body)
	}
}

// locks-queue-design-v1 §8: the reservation is the window record, so gates refuse others and the head's PUT is the renew path
func TestReservationBlocksOthersAndAdmitsHead(t *testing.T) {
	queueNow := time.Now().UTC().Truncate(time.Second) // decideReleaseWindow reads the wall clock
	_, cs := queueHandler(t)
	hold(t, cs, "seat-a", queueNow)
	queueUp(t, cs, "seat-b", "thread:local_b", 30, queueNow)
	if err := deleteReleaseWindow(context.Background(), cs, "cicd", "seat-a", false, queueNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	// A plain PUT on the free window reserves it for the head and is refused.
	_, err := putReleaseWindow(context.Background(), cs, "cicd", windowInput{Who: "seat-c", What: "bifrost-platform"}, queueNow.Add(time.Minute))
	if held, ok := err.(*WindowHeld); !ok || held.Who != "seat-b" {
		t.Fatalf("free window jumped the queue: %v", err)
	}
	r := rawWindow(t, cs)
	if r.Who != "seat-b" || r.Reason != "reserved" || r.Reserved == nil || len(r.Queue) != 0 {
		t.Fatalf("reservation: %+v", r)
	}
	if msg := decideReleaseWindow(true, r.asMap(), "bifrost-deliver-platform", "seat-c"); !strings.Contains(msg, "REFUSED") {
		t.Fatalf("gate let another in: %q", msg)
	}
	if n := advance(t, cs, queueNow.Add(time.Minute)); topics(n) != "window.your_turn>seat-b" {
		t.Fatalf("notices: %s", topics(n))
	}
	got, err := putReleaseWindow(context.Background(), cs, "cicd", windowInput{Who: "seat-b", What: "bifrost-platform", Reason: "deliver"}, queueNow.Add(90*time.Second))
	if err != nil || got.Reason != "deliver" || got.Reserved != nil || got.StartedAt != queueNow.Add(90*time.Second).Format(time.RFC3339) || got.Renewals != 0 {
		t.Fatalf("head PUT: %+v %v", got, err)
	}
	if msg := decideReleaseWindow(true, got.asMap(), "bifrost-deliver-platform", "seat-b"); msg != "" {
		t.Fatalf("gate refused the head: %q", msg)
	}
	if n := advance(t, cs, queueNow.Add(2*time.Minute)); len(n) != 0 {
		t.Fatalf("claimed reservation still notifies: %s", topics(n))
	}
}

// locks-queue-design-v1 §2: an unused reservation is skipped after 2 minutes and that who gets window.missed
func TestExpiredReservationMovesToNext(t *testing.T) {
	_, cs := queueHandler(t)
	hold(t, cs, "seat-a", queueNow)
	queueUp(t, cs, "seat-b", "thread:local_b", 30, queueNow)
	queueUp(t, cs, "seat-c", "thread:local_c", 30, queueNow)
	at := queueNow.Add(6 * time.Minute) // seat-a's TTL ran out
	if n := advance(t, cs, at); topics(n) != "window.your_turn>seat-b" {
		t.Fatalf("first reservation: %s", topics(n))
	}
	n := advance(t, cs, at.Add(2*time.Minute))
	if topics(n) != "window.missed>seat-b,window.your_turn>seat-c" {
		t.Fatalf("expired reservation: %s", topics(n))
	}
	if r := rawWindow(t, cs); r.Who != "seat-c" || r.Reserved == nil || len(r.Queue) != 0 {
		t.Fatalf("next reservation: %+v", r)
	}
	n = advance(t, cs, at.Add(4*time.Minute))
	if topics(n) != "window.missed>seat-c" {
		t.Fatalf("last reservation: %s", topics(n))
	}
	if n = advance(t, cs, at.Add(5*time.Minute)); len(n) != 0 {
		t.Fatalf("missed repeats: %s", topics(n))
	}
}

// ci-asks-locks.md: on queue timeout say who holds the window and until when
func TestQueueTimeoutNotifiesWithHolder(t *testing.T) {
	_, cs := queueHandler(t)
	if _, err := putReleaseWindow(context.Background(), cs, "cicd", windowInput{Who: "seat-a", What: "bifrost-platform", TTLMinutes: 60}, queueNow); err != nil {
		t.Fatal(err)
	}
	queueUp(t, cs, "seat-b", "thread:local_b", 1, queueNow)
	n := advance(t, cs, queueNow.Add(2*time.Minute))
	if topics(n) != "window.queue_timeout>seat-b" || n[0].Holder.Who != "seat-a" {
		t.Fatalf("timeout: %s %+v", topics(n), n)
	}
	body := windowEnvelope("cicd", n[0])
	var env map[string]any
	if json.Unmarshal([]byte(body), &env) != nil || env["type"] != "fact" || !strings.Contains(body, "topic:window.queue_timeout") ||
		!strings.Contains(body, "Holder: seat-a") || !strings.Contains(body, queueNow.Add(time.Hour).Format(time.RFC3339)) {
		t.Fatalf("envelope: %s", body)
	}
	if r := rawWindow(t, cs); len(r.Queue) != 0 || r.Who != "seat-a" {
		t.Fatalf("timed-out entry kept: %+v", r)
	}
}

// locks-queue-design-v1 §8: two enqueues racing under a CAS conflict both end up queued, once each
func TestConcurrentEnqueueUnderConflict(t *testing.T) {
	_, cs := queueHandler(t)
	hold(t, cs, "seat-a", queueNow)
	var mu sync.Mutex
	conflicts := 0
	cs.PrependReactor("update", "configmaps", func(kt.Action) (bool, runtime.Object, error) {
		mu.Lock()
		defer mu.Unlock()
		if conflicts == 0 {
			conflicts++
			return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, releaseWindowConfigMap, fmt.Errorf("stale"))
		}
		return false, nil, nil
	})
	whos := []string{"seat-b", "seat-c", "seat-d", "seat-e", "seat-f", "seat-g"}
	var wg sync.WaitGroup
	for _, who := range whos {
		wg.Add(1)
		go func(who string) {
			defer wg.Done()
			_, err := putReleaseWindow(context.Background(), cs, "cicd", windowInput{Who: who, What: "bifrost-platform", Queue: true}, queueNow)
			if _, ok := err.(*WindowQueued); !ok {
				t.Errorf("%s: %v", who, err)
			}
		}(who)
	}
	wg.Wait()
	r := rawWindow(t, cs)
	seen := map[string]int{}
	for _, e := range r.Queue {
		seen[e.Who]++
	}
	if conflicts != 1 || len(r.Queue) != len(whos) || len(seen) != len(whos) || r.Who != "seat-a" {
		t.Fatalf("queue after conflicts=%d: %+v", conflicts, r.Queue)
	}
}

// locks-queue-design-v1 §2: one position per who; queueing again refreshes until
func TestOnePositionPerWho(t *testing.T) {
	_, cs := queueHandler(t)
	hold(t, cs, "seat-a", queueNow)
	queueUp(t, cs, "seat-b", "", 10, queueNow)
	queueUp(t, cs, "seat-c", "", 10, queueNow)
	q := queueUp(t, cs, "seat-b", "", 20, queueNow.Add(time.Minute))
	if q.Position != 1 || len(q.Queue) != 2 || q.Queue[0].Until != queueNow.Add(21*time.Minute).Format(time.RFC3339) || q.Queue[0].Since != queueNow.Format(time.RFC3339) {
		t.Fatalf("requeue: %+v", q)
	}
}

// locks-queue-design-v1 §4: messages only to entries with a thread, and one per key
func TestNoticesOnlyWithThreadAndOncePerKey(t *testing.T) {
	h, cs := queueHandler(t)
	var sent []string
	h.SetWindowNotifier(func(_ context.Context, key, to, body string) error {
		sent = append(sent, key+">"+to)
		return nil
	})
	hold(t, cs, "seat-a", queueNow)
	queueUp(t, cs, "seat-c", "", 30, queueNow)
	queueUp(t, cs, "seat-b", "thread:local_b", 30, queueNow)
	at := queueNow.Add(6 * time.Minute)
	for i := 0; i < 2; i++ {
		h.notices.deliver(context.Background(), "cicd", advance(t, cs, at), at) // seat-c reserved: no thread
	}
	if len(sent) != 0 {
		t.Fatalf("sent to an entry without a thread: %v", sent)
	}
	at = at.Add(2 * time.Minute)
	for i := 0; i < 3; i++ {
		h.notices.deliver(context.Background(), "cicd", advance(t, cs, at.Add(time.Duration(i)*time.Second)), at)
	}
	want := "window:your_turn:seat-b:" + queueNow.Format(time.RFC3339) + ">thread:local_b"
	if len(sent) != 1 || sent[0] != want {
		t.Fatalf("sent %v, want [%s]", sent, want)
	}
}

// locks-queue-design-v1 §2: the holder's 3rd renewal while someone waits tells the head once
func TestStillHeldAfterThirdRenewal(t *testing.T) {
	_, cs := queueHandler(t)
	hold(t, cs, "seat-a", queueNow)
	queueUp(t, cs, "seat-b", "thread:local_b", 60, queueNow)
	for i := 1; i <= 3; i++ {
		if n := advance(t, cs, queueNow.Add(time.Duration(i)*time.Minute)); len(n) != 0 {
			t.Fatalf("before renewal %d: %s", i, topics(n))
		}
		hold(t, cs, "seat-a", queueNow.Add(time.Duration(i)*time.Minute))
	}
	n := advance(t, cs, queueNow.Add(4*time.Minute))
	if topics(n) != "window.still_held>seat-b" || n[0].Holder.Who != "seat-a" {
		t.Fatalf("still held: %s", topics(n))
	}
	if again := advance(t, cs, queueNow.Add(5*time.Minute)); again[0].Key != n[0].Key {
		t.Fatalf("still_held key changed: %s vs %s", again[0].Key, n[0].Key)
	}
}

// locks-queue-design-v1 §2: release and TTL expiry keep the queue
func TestQueueSurvivesReleaseAndExpiry(t *testing.T) {
	_, cs := queueHandler(t)
	hold(t, cs, "seat-a", queueNow)
	queueUp(t, cs, "seat-b", "", 60, queueNow)
	if err := deleteReleaseWindow(context.Background(), cs, "cicd", "seat-a", false, queueNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if r := rawWindow(t, cs); len(r.Queue) != 1 || r.EndedAt == "" {
		t.Fatalf("release dropped the queue: %+v", r)
	}
	// The head takes the free window directly and leaves the queue.
	hold(t, cs, "seat-b", queueNow.Add(time.Minute))
	queueUp(t, cs, "seat-c", "", 60, queueNow.Add(time.Minute))
	_, open, err := getReleaseWindow(context.Background(), cs, "cicd", queueNow.Add(10*time.Minute))
	if err != nil || open {
		t.Fatalf("expired window reads open: %v", err)
	}
	if r := rawWindow(t, cs); r.Who != "seat-b" || len(r.Queue) != 1 || r.Queue[0].Who != "seat-c" {
		t.Fatalf("expiry dropped the queue: %+v", r)
	}
}

// locks-queue-design-v1 §8: a queue-only write never fences a pipeline run's lease; other writes still do
func TestRunWindowUnaffectedByQueue(t *testing.T) {
	h, cs := queueHandler(t)
	ctx := context.Background()
	w, err := h.svc.takeRunWindow(ctx, "bifrost-deliver-platform", "run-1")
	if err != nil {
		t.Fatal(err)
	}
	queueUp(t, cs, "seat-b", "thread:local_b", 30, time.Now().UTC())
	if err := w.update(ctx, false); err != nil {
		t.Fatalf("queue write fenced the run: %v", err)
	}
	if r := rawWindow(t, cs); r.Who != "run-1" || len(r.Queue) != 1 {
		t.Fatalf("renewal lost the queue: %+v", r)
	}
	// A version bump that is not a queue write still fences, as before.
	cm, _ := cs.CoreV1().ConfigMaps("cicd").Get(ctx, releaseWindowConfigMap, metav1.GetOptions{})
	cm.Annotations = map[string]string{"bump": "1"}
	if _, err := cs.CoreV1().ConfigMaps("cicd").Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	cm, _ = cs.CoreV1().ConfigMaps("cicd").Get(ctx, releaseWindowConfigMap, metav1.GetOptions{})
	w.version = "stale-" + cm.ResourceVersion
	if err := w.update(ctx, true); !windowFenced(err) {
		t.Fatalf("stale lease not fenced: %v", err)
	}
	if err := (&runWindow{cs: cs, ns: "cicd", who: "run-1", version: cm.ResourceVersion, lease: w.lease, queueRev: w.queueRev}).update(ctx, true); err != nil {
		t.Fatalf("release: %v", err)
	}
	if r := rawWindow(t, cs); r.EndedAt == "" || len(r.Queue) != 1 {
		t.Fatalf("run end dropped the queue: %+v", r)
	}
}
