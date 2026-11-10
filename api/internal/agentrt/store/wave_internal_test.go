package store

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWaveBodies(t *testing.T) {
	t.Parallel()
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	forms := map[string]map[string]any{
		"wave_opened":         (WaveOpened{Wave: "w", Repo: "bifrost-platform", Ref: "refs/heads/wave/R6/1", PlanRoot: "R6", N: 1, OpenedBy: "owner", Base: a}).body(),
		"wave_appended":       (WaveAppended{Wave: "w", Task: "t", Commit: b, Parent: a, ChangeID: "change"}).body(),
		"wave_append_refused": (WaveAppendRefused{Wave: "w", Task: "t", Why: "excluded"}).body(),
		"wave_closed":         (WaveClosed{Wave: "w", By: "owner"}).body(),
		"wave_landing":        (WaveLanding{Wave: "w", Batch: "batch", M0: a, R: b}).body(),
		"wave_red":            (WaveRed{Wave: "w", Batch: "batch"}).body(),
		"wave_landed":         (WaveLanded{Wave: "w", R: b, Main: b}).body(),
		"wave_abandoned":      (WaveAbandoned{Wave: "w", By: "owner", Why: "cancelled"}).body(),
		"wave_tally":          (WaveTallyNote{Wave: "w", LandingCI: 1, Stg: 2}).body(),
	}
	for name, body := range forms {
		t.Run(name, func(t *testing.T) {
			if why := badWave(name, body); why != "" {
				t.Fatal(why)
			}
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			var decoded map[string]any
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatal(err)
			}
			if why := badWave(name, decoded); why != "" {
				t.Fatal(why)
			}
			for key := range body {
				bad := map[string]any{}
				for k, v := range body {
					bad[k] = v
				}
				delete(bad, key)
				if badWave(name, bad) == "" {
					t.Fatalf("accepted missing %s", key)
				}
				bad[key] = nil
				if badWave(name, bad) == "" {
					t.Fatalf("accepted null %s", key)
				}
			}
			body["v"] = "2"
			if badWave(name, body) == "" {
				t.Fatal("accepted v2")
			}
		})
	}
	for _, n := range []any{-1, 1.5, "1", true} {
		if badWave("wave_tally", map[string]any{"v": "1", "wave": "w", "landing_ci": n, "stg": 0, "prod": 0}) == "" {
			t.Fatalf("accepted tally %v", n)
		}
	}
	if err := checkBody("task_proposed", map[string]any{"title": "task", "role": "implement", "wave": "w"}); err != nil {
		t.Fatal(err)
	}
	if err := checkBody("task_proposed", map[string]any{"title": "task", "role": "implement", "wave": 1}); err == nil {
		t.Fatal("accepted numeric wave key")
	}
}
