package assessmentview

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func originalRead(ctx context.Context, read func(context.Context) (json.RawMessage, error)) map[string]any {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	events, err := read(ctx)
	if err != nil {
		return map[string]any{"error": "could not read the assessment record"}
	}
	return map[string]any{"events": events}
}
func TestRecordedResponseMatchesOriginal(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, json.RawMessage("[]"), json.RawMessage(`[{"event_type":"note","body_json":{"text":"observed"}}]`)} {
		for _, err := range []error{nil, errors.New("read failed")} {
			read := func(ctx context.Context) (json.RawMessage, error) {
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 2*time.Second || time.Until(deadline) < time.Second {
					t.Fatal("read budget changed")
				}
				return raw, err
			}
			want, got := originalRead(context.Background(), read), Read(context.Background(), read)
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("%#v != %#v", want, got)
			}
		}
	}
}
func TestRecordedResponseCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	read := func(ctx context.Context) (json.RawMessage, error) { return nil, ctx.Err() }
	if !reflect.DeepEqual(originalRead(ctx, read), Read(ctx, read)) {
		t.Fatal("cancel response changed")
	}
}
