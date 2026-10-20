package store_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// oldRework is the next round's text as reworkSpec wrote it before
// ReworkText was taken out of it, why worded by refusePlan.
func oldRework(text string, round int, why string) string {
	return text + fmt.Sprintf("\n\n---\n## Round %d\n\nThe plan you handed in before was not used: %s\n\n"+
		"Hand in a plan that passes; everything above still holds.\n", round, strings.TrimPrefix(why, "plan not used: "))
}

// refusal is how refusePlan words a reason: one line, cut at 500 runes.
func refusal(why string) string {
	s := strings.Join(strings.Fields("plan not used: "+why), " ")
	if utf8.RuneCountInString(s) <= 500 {
		return s
	}
	return string([]rune(s)[:499]) + "…"
}

// The chief exam's S4: agentd exam gives a refused plan its second round
// with ReworkText, and the spec the controller writes is byte for byte what
// it wrote before ReworkText was taken out of reworkSpec, whatever the
// reason: empty, on several lines, longer than the cut, in Chinese, or
// worded with the refusal's own prefix.
func TestReworkTextIsTheControllersRework(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	reasons := []string{"the report has no ```json block", "", " \n\t ", "tasks[0]: the title\nis on two lines\tand tabbed",
		strings.Repeat("长", 600), strings.Repeat("x ", 400), "plan not used: given twice", "questions[1]: …"}
	specs := map[string]string{
		"json":     `{"siblings":[{"ref":"main","repo":"bifrost-ui"}],"text":"# Plan the goal\n\nRead it."}`,
		"markdown": "# Plan the goal\n\nRead it.\n",
	}
	for kind, spec := range specs {
		var id string
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			var err error
			id, err = store.PutArtifact(ctx, tx, "controller", "task_spec", "application/json", []byte(spec))
			return err
		}))
		text := spec
		doc := map[string]any{}
		if kind == "json" {
			ok(t, json.Unmarshal([]byte(spec), &doc))
			text = doc["text"].(string)
		}
		for _, why := range reasons {
			got, err := store.ReworkSpecAsRefused(ctx, w.rt, id, 2, why)
			ok(t, err)
			doc["text"] = oldRework(text, 2, refusal(why))
			want, err := canon.Encode(doc)
			ok(t, err)
			if string(got) != string(want) {
				t.Fatalf("%s, %q: the controller's spec changed:\n%s\nwas\n%s", kind, why, got, want)
			}
			var back struct{ Text string }
			ok(t, json.Unmarshal(got, &back))
			if back.Text != store.ReworkText(text, 2, why) {
				t.Fatalf("%s, %q: ReworkText is not the controller's text:\n%q\n%q", kind, why, store.ReworkText(text, 2, why), back.Text)
			}
		}
	}
}
