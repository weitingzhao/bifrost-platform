package server

import (
	"context"
	"testing"
)

func TestSupersedeRequiresApprovalAndProd(t *testing.T) {
	s := &Server{}
	for _, p := range []map[string]any{
		{"env": "stg", "entries": []any{}, "reason": "x"},
		{"env": "prod", "entries": []any{}, "reason": "x"},
		{"env": "prod", "entries": "not an array", "reason": "x"},
		{"env": "prod", "entries": []any{}, "reason": "x", "unexpected": true},
	} {
		if err := s.supersedeCreateCheck(context.Background(), p); err == nil {
			t.Fatal("accepted invalid request")
		}
	}
	if _, err := s.supersedeTasks(context.Background(), map[string]any{}); err == nil {
		t.Fatal("executor accepted direct call")
	}
}
