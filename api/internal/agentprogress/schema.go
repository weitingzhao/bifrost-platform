package agentprogress

import (
	"encoding/json"
	"fmt"
)

// Caps on the arrays the Console walks. A larger array is a 400, not a
// truncated document: the publisher should send a smaller board.
const (
	maxWorks    = 30
	maxSteps    = 20
	maxOwners   = 10
	maxActions  = 10
	maxFeed     = 50
	maxRecent   = 30
	maxNeedsYou = 30
)

// validateDocument checks the known fields. Unknown fields are left in place.
// A wrong JSON type, or an array over its cap, is an error whose text includes
// the field path.
func validateDocument(obj map[string]any) error {
	if err := wantNumber(obj, "", "at"); err != nil {
		return err
	}
	if err := wantString(obj, "", "verdict"); err != nil {
		return err
	}
	if err := wantString(obj, "", "level"); err != nil {
		return err
	}
	if err := wantBool(obj, "", "sure"); err != nil {
		return err
	}
	if err := wantString(obj, "", "host"); err != nil {
		return err
	}
	if err := validateCoordinator(obj); err != nil {
		return err
	}
	if err := validateOwnerNext(obj, "", "owner_next"); err != nil {
		return err
	}
	if err := validateOwnerNow(obj); err != nil {
		return err
	}
	if err := validateStringArray(obj, "", "needs_you", maxNeedsYou); err != nil {
		return err
	}
	if err := validateWorks(obj); err != nil {
		return err
	}
	if err := validateFeed(obj); err != nil {
		return err
	}
	return validateStringArray(obj, "", "recent", maxRecent)
}

func validateCoordinator(obj map[string]any) error {
	child, ok, err := objectOrNull(obj, "", "coordinator")
	if err != nil || !ok {
		return err
	}
	if err := wantNumber(child, "coordinator", "at"); err != nil {
		return err
	}
	if err := wantString(child, "coordinator", "state"); err != nil {
		return err
	}
	return wantString(child, "coordinator", "note")
}

func validateOwnerNext(obj map[string]any, path, key string) error {
	child, ok, err := objectOrNull(obj, path, key)
	if err != nil || !ok {
		return err
	}
	base := join(path, key)
	if err := wantString(child, base, "name"); err != nil {
		return err
	}
	if err := wantNumber(child, base, "at"); err != nil {
		return err
	}
	if key == "next" {
		if err := wantString(child, base, "who"); err != nil {
			return err
		}
		return wantBool(child, base, "needs_owner")
	}
	return wantString(child, base, "work")
}

func validateOwnerNow(obj map[string]any) error {
	items, err := arrayOf(obj, "", "owner_now", maxOwners, "objects")
	if err != nil || items == nil {
		return err
	}
	for i, item := range items {
		path := fmt.Sprintf("owner_now[%d]", i)
		entry, err := asObject(item, path)
		if err != nil {
			return err
		}
		if err := wantString(entry, path, "name"); err != nil {
			return err
		}
		if err := wantString(entry, path, "urgency"); err != nil {
			return err
		}
		if err := wantNumber(entry, path, "since"); err != nil {
			return err
		}
		if err := validateActions(entry, path); err != nil {
			return err
		}
	}
	return nil
}

func validateActions(entry map[string]any, path string) error {
	items, err := arrayOf(entry, path, "actions", maxActions, "objects")
	if err != nil || items == nil {
		return err
	}
	for i, item := range items {
		actionPath := fmt.Sprintf("%s.actions[%d]", path, i)
		action, err := asObject(item, actionPath)
		if err != nil {
			return err
		}
		if err := wantString(action, actionPath, "text"); err != nil {
			return err
		}
		if err := wantBool(action, actionPath, "done"); err != nil {
			return err
		}
		if err := wantString(action, actionPath, "link"); err != nil {
			return err
		}
		if err := wantString(action, actionPath, "link_text"); err != nil {
			return err
		}
	}
	return nil
}

func validateWorks(obj map[string]any) error {
	items, err := arrayOf(obj, "", "works", maxWorks, "objects")
	if err != nil || items == nil {
		return err
	}
	for i, item := range items {
		path := fmt.Sprintf("works[%d]", i)
		work, err := asObject(item, path)
		if err != nil {
			return err
		}
		for _, key := range []string{"title", "short", "goal", "risk", "after"} {
			if err := wantString(work, path, key); err != nil {
				return err
			}
		}
		for _, key := range []string{"round", "end"} {
			if err := wantNumber(work, path, key); err != nil {
				return err
			}
		}
		for _, key := range []string{"sure", "finished"} {
			if err := wantBool(work, path, key); err != nil {
				return err
			}
		}
		if err := validateOwnerNext(work, path, "next"); err != nil {
			return err
		}
		if err := validateSteps(work, path); err != nil {
			return err
		}
	}
	return nil
}

func validateSteps(work map[string]any, path string) error {
	items, err := arrayOf(work, path, "steps", maxSteps, "objects")
	if err != nil || items == nil {
		return err
	}
	for i, item := range items {
		stepPath := fmt.Sprintf("%s.steps[%d]", path, i)
		step, err := asObject(item, stepPath)
		if err != nil {
			return err
		}
		for _, key := range []string{"name", "who", "state", "label", "note", "doing"} {
			if err := wantString(step, stepPath, key); err != nil {
				return err
			}
		}
		for _, key := range []string{"started", "ended", "last", "usual_min", "eta_start", "eta_end", "overdue"} {
			if err := wantNumber(step, stepPath, key); err != nil {
				return err
			}
		}
		if err := wantBool(step, stepPath, "needs_owner"); err != nil {
			return err
		}
	}
	return nil
}

func validateFeed(obj map[string]any) error {
	items, err := arrayOf(obj, "", "feed", maxFeed, "objects")
	if err != nil || items == nil {
		return err
	}
	for i, item := range items {
		path := fmt.Sprintf("feed[%d]", i)
		row, err := asObject(item, path)
		if err != nil {
			return err
		}
		if err := wantNumber(row, path, "at"); err != nil {
			return err
		}
		for _, key := range []string{"work", "step", "who", "state", "text"} {
			if err := wantString(row, path, key); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateStringArray(obj map[string]any, path, key string, max int) error {
	items, err := arrayOf(obj, path, key, max, "strings")
	if err != nil || items == nil {
		return err
	}
	base := join(path, key)
	for i, item := range items {
		if _, ok := item.(string); !ok {
			return fmt.Errorf("%s[%d]: want a string", base, i)
		}
	}
	return nil
}

// arrayOf returns the array, or nil when the field is absent. A present null
// or other type is an error. Over the cap is an error.
func arrayOf(obj map[string]any, path, key string, max int, kind string) ([]any, error) {
	v, present := obj[key]
	if !present {
		return nil, nil
	}
	items, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: want an array of %s", join(path, key), kind)
	}
	if len(items) > max {
		return nil, fmt.Errorf("%s: %d items exceeds %d", join(path, key), len(items), max)
	}
	return items, nil
}

func asObject(v any, path string) (map[string]any, error) {
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: want an object", path)
	}
	return obj, nil
}

// objectOrNull accepts a missing field and JSON null. ok is true when there
// is an object to walk.
func objectOrNull(obj map[string]any, path, key string) (map[string]any, bool, error) {
	v, present := obj[key]
	if !present || v == nil {
		return nil, false, nil
	}
	child, ok := v.(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("%s: want an object or null", join(path, key))
	}
	return child, true, nil
}

func wantString(obj map[string]any, path, key string) error {
	return wantScalar(obj, path, key, "a string", func(v any) bool {
		_, ok := v.(string)
		return ok
	})
}

func wantNumber(obj map[string]any, path, key string) error {
	return wantScalar(obj, path, key, "a number", func(v any) bool {
		_, ok := v.(json.Number)
		return ok
	})
}

func wantBool(obj map[string]any, path, key string) error {
	return wantScalar(obj, path, key, "a boolean", func(v any) bool {
		_, ok := v.(bool)
		return ok
	})
}

func wantScalar(obj map[string]any, path, key, what string, match func(any) bool) error {
	v, present := obj[key]
	if !present {
		return nil
	}
	if !match(v) {
		return fmt.Errorf("%s: want %s", join(path, key), what)
	}
	return nil
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}
