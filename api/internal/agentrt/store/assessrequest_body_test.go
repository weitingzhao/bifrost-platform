package store

import "testing"

// TD-463: exercise the actual event body validator without PostgreSQL so a
// missing field registration cannot hide behind skipped integration tests.
func TestAssessmentRequestNoteBody(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{AssessCard, AssessApproval} {
		request := map[string]any{"kind": kind, "id": "463"}
		if err := checkBody("note", map[string]any{"text": "Owner requested advice", "assessment_requested": request}); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if err := checkBody("note", map[string]any{"assessment_requested": request}); err == nil {
			t.Fatal("a request note without text was accepted")
		}
		if err := checkBody("note", map[string]any{"text": "Owner requested advice", "assessment_requested": request, "unknown": true}); err == nil {
			t.Fatal("an unregistered note field was accepted")
		}
	}
	for name, request := range map[string]any{
		"null":         nil,
		"string":       "card:463",
		"missing kind": map[string]any{"id": "463"},
		"missing id":   map[string]any{"kind": AssessCard},
		"wrong kind":   map[string]any{"kind": "mission", "id": "463"},
		"empty id":     map[string]any{"kind": AssessCard, "id": " "},
		"numeric id":   map[string]any{"kind": AssessApproval, "id": 463},
		"extra field":  map[string]any{"kind": AssessCard, "id": "463", "state": "approved"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := checkBody("note", map[string]any{"text": "Owner requested advice", "assessment_requested": request}); err == nil {
				t.Fatal("an invalid request record was accepted")
			}
		})
	}
}
