package actions

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
)

const attempt = "a_01M56J5QPAAQQP53KV4MQYCW5Q"

func code(n int) *int { return &n }

func TestNormalizeKeepsTheFormatClosed(t *testing.T) {
	now := time.Date(2026, 10, 18, 5, 0, 0, 0, time.UTC)
	ok := Action{Kind: Read, Target: "api/internal/safego/safego.go", At: now}
	cases := map[string]Action{
		"unknown kind":        {Kind: "think", Target: "x", At: now},
		"no target":           {Kind: Read, At: now},
		"no time":             {Kind: Read, Target: "x"},
		"time in the future":  {Kind: Read, Target: "x", At: now.Add(10 * time.Minute)},
		"exit code on a read": {Kind: Read, Target: "x", At: now, Code: code(1)},
		"target not utf-8":    {Kind: Run, Target: "ls \xff", At: now},
		"exit code on a run":  {Kind: Run, Target: "go test ./...", At: now, Code: code(0)},
	}
	for name, bad := range cases {
		if _, _, err := Normalize([]Action{ok, bad}, now); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	got, dropped, err := Normalize([]Action{ok, {Kind: Result, Target: "go test ./...", At: now, Code: code(1)}}, now)
	if err != nil || dropped != 0 || len(got) != 2 || *got[1].Code != 1 {
		t.Fatalf("got %+v, %d, %v", got, dropped, err)
	}
}

func TestNormalizeCutsLongTargetsAndKeepsTheNewest(t *testing.T) {
	now := time.Now()
	long := strings.Repeat("é", MaxTarget+10)
	got, _, err := Normalize([]Action{{Kind: Run, Target: long, At: now}}, now)
	if err != nil || !got[0].Truncated || len([]rune(got[0].Target)) != MaxTarget {
		t.Fatalf("got %d runes, truncated %v, err %v", len([]rune(got[0].Target)), got[0].Truncated, err)
	}
	var many []Action
	for i := 0; i < MaxPerBeat+7; i++ {
		many = append(many, Action{Kind: Read, Target: "f" + string(rune('a'+i%26)), At: now.Add(time.Duration(i) * time.Millisecond)})
	}
	got, dropped, err := Normalize(many, now.Add(time.Second))
	if err != nil || dropped != 7 || len(got) != MaxPerBeat || !got[len(got)-1].At.Equal(many[len(many)-1].At.UTC()) {
		t.Fatalf("kept %d, dropped %d, err %v", len(got), dropped, err)
	}
}

func TestStoreRefusesAKeyThatIsNotAnAttempt(t *testing.T) {
	s := &Store{Prefix: "agrt:"}
	for _, id := range []string{"", "a_short", "t_01M56J5QPAAQQP53KV4MQYCW5Q", "a_01M56J5QPAAQQP53KV4MQYCW5Q:x", "a_01M56J5QPAAQQP53KV4MQYCWIL"} {
		if _, err := s.key(id); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: err = %v", id, err)
		}
	}
}

func TestStoreAppendsReadsAndExpires(t *testing.T) {
	rdb, prefix := agentrttest.Redis(t)
	ctx := context.Background()
	s := &Store{Redis: rdb, Prefix: prefix}
	now := time.Now().UTC().Truncate(time.Millisecond)
	if _, ok, err := s.Last(ctx, attempt); ok || err != nil {
		t.Fatalf("empty stream: ok %v err %v", ok, err)
	}
	first := []Action{{Kind: Read, Target: "a.go", At: now}, {Kind: Run, Target: "go vet ./...", At: now}}
	if err := s.Append(ctx, attempt, first); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(ctx, attempt, []Action{{Kind: Result, Target: "go vet ./...", At: now, Code: code(2), Truncated: true}}); err != nil {
		t.Fatal(err)
	}
	all, err := s.Read(ctx, attempt, "", 100)
	if err != nil || len(all) != 3 || all[0].Target != "a.go" || all[2].Code == nil || *all[2].Code != 2 || !all[2].Truncated || !all[1].At.Equal(now) {
		t.Fatalf("read %+v, %v", all, err)
	}
	after, err := s.Read(ctx, attempt, all[0].ID, 100)
	if err != nil || len(after) != 2 || after[0].ID != all[1].ID {
		t.Fatalf("after the first: %+v, %v", after, err)
	}
	last, ok, err := s.Last(ctx, attempt)
	if err != nil || !ok || last.ID != all[2].ID {
		t.Fatalf("last %+v %v %v", last, ok, err)
	}
	ttl, err := rdb.TTL(ctx, prefix+"act:"+attempt).Result()
	if err != nil || ttl <= TTL-time.Minute || ttl > TTL {
		t.Fatalf("ttl %v %v", ttl, err)
	}
}

func TestStoreTrimsTheStream(t *testing.T) {
	rdb, prefix := agentrttest.Redis(t)
	ctx := context.Background()
	s := &Store{Redis: rdb, Prefix: prefix}
	now := time.Now().UTC()
	for i := 0; i < 3*MaxStream/MaxPerBeat; i++ {
		batch := make([]Action, MaxPerBeat)
		for j := range batch {
			batch[j] = Action{Kind: Read, Target: "f.go", At: now}
		}
		if err := s.Append(ctx, attempt, batch); err != nil {
			t.Fatal(err)
		}
	}
	n, err := rdb.XLen(ctx, prefix+"act:"+attempt).Result()
	// approximate trimming keeps at least MaxStream and drops whole nodes only
	if err != nil || n < MaxStream || n >= 3*MaxStream {
		t.Fatalf("stream length %d, %v", n, err)
	}
}
