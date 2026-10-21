package migrate

import (
	"slices"
	"testing"
)

// An applied migration with other bytes is the worst state, before one
// missing; the lists name every case.
func TestCompareOrdersTheStates(t *testing.T) {
	files := []File{{ID: 1, Name: "0001_a.sql", SHA256: "x1"}, {ID: 2, Name: "0002_b.sql", SHA256: "x2"}}
	for name, c := range map[string]struct {
		applied map[int]string
		want    SchemaStatus
	}{
		"current": {map[int]string{1: "x1", 2: "x2"}, SchemaStatus{State: SchemaCurrent}},
		"behind":  {map[int]string{1: "x1"}, SchemaStatus{State: SchemaBehind, Missing: []string{"0002_b"}}},
		"ahead":   {map[int]string{1: "x1", 2: "x2", 3: "x3"}, SchemaStatus{State: SchemaAhead, Unknown: []int{3}}},
		"edited over behind": {map[int]string{1: "other"},
			SchemaStatus{State: SchemaEdited, Missing: []string{"0002_b"}, Edited: []string{"0001_a"}}},
	} {
		got := compare(files, c.applied)
		if got.State != c.want.State || !slices.Equal(got.Missing, c.want.Missing) || !slices.Equal(got.Unknown, c.want.Unknown) ||
			!slices.Equal(got.Edited, c.want.Edited) {
			t.Errorf("%s: %+v; want %+v", name, got, c.want)
		}
	}
}
