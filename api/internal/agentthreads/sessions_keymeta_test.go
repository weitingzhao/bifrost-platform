package agentthreads

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

func TestHeldKeyFirstSeenFromOldRows(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		first time.Time
		want  time.Time
	}{
		{name: "all old rows", want: t0},
		{name: "explicit first sighting", first: t0.Add(time.Minute), want: t0.Add(time.Minute)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			st := State{Threads: map[string]Thread{
				Key("claude", "s"):   {Thread: "s", Host: "h1", Vendor: "claude", Session: "s", Epoch: 1, At: t0, FirstSeenAt: tc.first},
				Key("claude", "s:1"): {Thread: "s:1", Host: "h1", Vendor: "claude", Session: "s", Epoch: 1, At: t0.Add(2 * time.Minute)},
			}}
			got := st.heldKey("", "h1", "claude", "s", 1)
			if got == nil || !got.FirstSeenAt.Equal(tc.want) {
				t.Fatalf("held key = %+v, want first seen %v", got, tc.want)
			}
		})
	}
}

// P-c2 landing review rounds 1 and 2: a session key's state (its first
// sighting, the precompacts counted for it, the client's last diagnosis) is
// one copy that every row of the key carries. A session recovered under
// another thread id (<session>:n) must neither hide a later refusal nor count
// a precompact on a stale copy, whatever the order of beats across its rows.
//
// Random interleavings over three rows of one key: s from the start, s:1 and
// s:2 joining as recovered rows. Each step is a turn beat, a precompact (a
// side beat that leaves a row's clock alone in a turn) or a beat carrying a
// client diagnosis, on any row already heard. After every step every row of
// the key must equal the model, and the session counts must judge the key
// by the model's diagnosis.
func TestSessionKeyMetadataInterleavings(t *testing.T) {
	rows := []string{"s", "s:1", "s:2"}
	diag := []*ClientBind{{State: BindRefused, Reason: "no_context"}, {State: BindUnlinked, Reason: "no_description"}, {State: BindLinked}}
	for seed := int64(1); seed <= 300; seed++ {
		rng := rand.New(rand.NewSource(seed))
		st := State{}
		seq := map[string]uint64{}
		inTurn := map[string]bool{}
		heard := []string{}
		var wantCompactions int
		var wantBind *ClientBind
		var first time.Time
		trace := ""
		at := t0
		for step := 0; step < 24; step++ {
			at = at.Add(time.Duration(1+rng.Intn(90)) * time.Second)
			var row string
			if len(heard) < len(rows) && (len(heard) == 0 || rng.Intn(4) == 0) {
				row = rows[len(heard)]
				heard = append(heard, row)
			} else {
				row = heard[rng.Intn(len(heard))]
			}
			seq[row]++
			b := Beat{Thread: row, Vendor: "claude", Host: "h1", Session: "s", Epoch: 1, Seq: seq[row], TurnID: "turn-" + row}
			switch k := rng.Intn(5); {
			case k == 0 || seq[row] == 1:
				b.Event = TurnStart
				inTurn[row] = true
			case k == 1:
				b.Event, b.Reason = PreCompact, "auto"
				wantCompactions++
			case k == 2:
				b.Event = AfterTool
				if !inTurn[row] {
					b.Event = TurnStart
					inTurn[row] = true
				}
				cb := *diag[rng.Intn(len(diag))]
				b.ClientBind = &cb
				wantBind = &cb
			case k == 3:
				b.Event = TurnEnd
				inTurn[row] = false
			default:
				b.Event = AfterTool
				if !inTurn[row] {
					b.Event = TurnStart
					inTurn[row] = true
				}
			}
			if !st.Apply(b, at) {
				t.Fatalf("seed %d step %d: %s %s on %s not stored", seed, step, b.Event, fmt.Sprint(b.Seq), row)
			}
			if first.IsZero() {
				first = at
			}
			trace += fmt.Sprintf(" %s:%s", row, b.Event)
			if b.ClientBind != nil {
				trace += "+" + b.ClientBind.State
			}
			for _, r := range heard {
				got := st.Threads[Key("claude", r)]
				if got.Compactions != wantCompactions || !sameBind(got.ClientBind, wantBind) || !got.FirstSeenAt.Equal(first) {
					t.Fatalf("seed %d after%s: row %s has compactions=%d client_bind=%v first_seen=%v; want %d %v %v",
						seed, trace, r, got.Compactions, got.ClientBind, got.FirstSeenAt, wantCompactions, wantBind, first)
				}
			}
			counts := DefaultConfig().sessionCounts(st, indexWorks(nil, nil))
			refused := 0
			if wantBind != nil && wantBind.State == BindRefused {
				refused = 1
			}
			if counts.Refused != refused || counts.Refused+counts.Unlinked != 1 {
				t.Fatalf("seed %d after%s: counts %+v, want refused=%d", seed, trace, counts, refused)
			}
		}
	}
}

func sameBind(a, b *ClientBind) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// The second review's two probes, as named cases.
func TestRecoveredRowsShareOneCopy(t *testing.T) {
	st := State{}
	b := Beat{Thread: "s", Vendor: "claude", Host: "h1", Session: "s", Epoch: 1}
	b.Event, b.Seq, b.TurnID = TurnStart, 1, "t"
	st.Apply(b, t0)
	b.Event, b.Seq = PreCompact, 2
	st.Apply(b, t0.Add(time.Minute))
	r := Beat{Thread: "s:1", Vendor: "claude", Host: "h1", Session: "s", Epoch: 1, Event: TurnStart, Seq: 1, TurnID: "u"}
	st.Apply(r, t0.Add(2*time.Minute))
	// A refusal on s inside its turn: s:1 carries it too, so neither the
	// session counts nor a list that picks s:1 by its clock can hide it.
	b.Event, b.Seq, b.ClientBind = PreCompact, 3, &ClientBind{State: BindRefused, Reason: "no_context"}
	st.Apply(b, t0.Add(3*time.Minute))
	if got := DefaultConfig().sessionCounts(st, indexWorks(nil, nil)); got.Refused != 1 || got.Unlinked != 0 {
		t.Fatalf("a later refusal on s was hidden by s:1: %+v", got)
	}
	if got := st.Threads[Key("claude", "s:1")]; got.Compactions != 2 || got.ClientBind == nil || got.ClientBind.State != BindRefused {
		t.Fatalf("s:1 does not carry the key's state: %+v", got)
	}
	// A precompact on s:1 counts the third attempt, not a second on a stale copy.
	r.Event, r.Seq = PreCompact, 2
	st.Apply(r, t0.Add(4*time.Minute))
	for _, row := range []string{"s", "s:1"} {
		if got := st.Threads[Key("claude", row)]; got.Compactions != 3 || got.ClientBind == nil || got.ClientBind.State != BindRefused {
			t.Fatalf("%s: compactions=%d client_bind=%v, want 3 and the refusal", row, got.Compactions, got.ClientBind)
		}
	}
}

// A buffered beat that opens a row the cap refuses changes nothing: the
// diagnosis it carried is taken back from the key's other rows too
// (foldBatch restores the copies shareKeyMeta wrote). Every other row is in
// a turn, so none can make room, and the new row is never its own victim
// (P-c2 pre-review M2): it is refused whether or not its beat opens a turn.
func TestRefusedRowLeavesKeyStateAlone(t *testing.T) {
	st := State{}
	s := Beat{Thread: "s", Vendor: "claude", Host: "h1", Session: "s", Epoch: 1, Event: TurnStart, Seq: 1, TurnID: "t"}
	st.Apply(s, t0)
	for i := 1; len(st.Threads) < maxThreads; i++ {
		st.Apply(Beat{Thread: fmt.Sprintf("busy-%d", i), Vendor: "claude", Host: "h1", Event: TurnStart, Seq: 1, TurnID: "b"}, t0)
	}
	before := st.Threads[Key("claude", "s")]
	refused, _ := foldBatch(&st, []stamped{{beat: Beat{Thread: "s:1", Vendor: "claude", Host: "h1", Session: "s", Epoch: 1, Event: TurnStart,
		TurnID: "u", Seq: 1, ClientBind: &ClientBind{State: BindRefused, Reason: "no_context"}}, at: t0.Add(time.Minute)}}, nil, t0.Add(time.Minute))
	if refused != 1 {
		t.Fatalf("the new row was not refused (refused=%d)", refused)
	}
	if _, ok := st.Threads[Key("claude", "s:1")]; ok {
		t.Fatal("the refused row was kept")
	}
	if got := st.Threads[Key("claude", "s")]; got.Compactions != before.Compactions || got.ClientBind != nil || !got.MetaAt.Equal(before.MetaAt) {
		t.Fatalf("a refused row's beat changed its key's state on s: %+v", got)
	}
}
