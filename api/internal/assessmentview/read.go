package assessmentview

import (
	"context"
	"encoding/json"
	"time"
)

// Read bounds one optional persisted-advice read; it never initiates assessment work.
func Read(ctx context.Context, read func(context.Context) (json.RawMessage, error)) map[string]any {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	events, err := read(ctx)
	if err != nil {
		return map[string]any{"error": "could not read the assessment record"}
	}
	return map[string]any{"events": events}
}
