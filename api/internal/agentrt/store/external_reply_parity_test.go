package store

import (
	"bytes"
	"encoding/json"
	"net/url"
	"reflect"
	"testing"
)

func originalExternalReply(n externalNote, event int64) map[string]any {
	out := map[string]any{"external_work_id": n.ID, "event_id": event, "seq": n.Seq, "state": n.State, "plan_url": "#work?section=plan&root=" + url.QueryEscape(n.Root) + "&node=" + url.QueryEscape(n.Node)}
	// When it was registered (R5:5.5-12 §3.3): every note of the work
	// carries its registration's time, so a replay and a recovery read give
	// the first answer's.
	if n.RegisteredAt != "" {
		out["registered_at"] = n.RegisteredAt
	}
	return out
}

func TestExternalReplyMatchesOriginal(t *testing.T) {
	for _, root := range []string{"", "round-1", "a b&x=1/中文?"} {
		for _, node := range []string{"", "step", "a+b/#?"} {
			for _, state := range []string{"Registered", "Running", "Submitted", "Cancelled", "Unknown", ""} {
				for _, at := range []string{"", "2026-11-16T18:00:00.123456Z"} {
					for _, event := range []int64{0, 91} {
						n := externalNote{ID: "work-id", Root: root, Node: node, State: state, RegisteredAt: at, Seq: event, Actor: "not-in-response", RequestDigest: "not-in-response"}
						want, got := originalExternalReply(n, event), externalReply(n, event)
						if !reflect.DeepEqual(want, got) {
							t.Fatalf("map mismatch %#v %#v", want, got)
						}
						oldJSON, err := json.Marshal(want)
						if err != nil {
							t.Fatal(err)
						}
						newJSON, err := json.Marshal(got)
						if err != nil {
							t.Fatal(err)
						}
						if !bytes.Equal(oldJSON, newJSON) {
							t.Fatalf("JSON %s != %s", oldJSON, newJSON)
						}
					}
				}
			}
		}
	}
}
