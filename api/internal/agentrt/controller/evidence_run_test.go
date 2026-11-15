package controller

import (
	"reflect"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// fill sets every field of the struct v points at to a value that is not its
// zero, by kind; it fails on a kind it does not know, so a new kind of field
// is looked at and not skipped.
func fill(t *testing.T, v reflect.Value) {
	t.Helper()
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		switch {
		case f.Type() == reflect.TypeOf(time.Time{}):
			f.Set(reflect.ValueOf(time.Unix(1700000000, 0).UTC()))
		case f.Kind() == reflect.String:
			f.SetString(v.Type().Field(i).Name)
		case f.Kind() == reflect.Bool:
			f.SetBool(true)
		case f.Kind() == reflect.Slice && f.Type().Elem().Kind() == reflect.String:
			f.Set(reflect.ValueOf([]string{v.Type().Field(i).Name}))
		case f.Kind() == reflect.Slice && f.Type().Elem().Kind() == reflect.Struct:
			e := reflect.New(f.Type().Elem()).Elem()
			fill(t, e)
			f.Set(reflect.Append(reflect.MakeSlice(f.Type(), 0, 1), e))
		default:
			t.Fatalf("field %s has kind %s, which fill does not know", v.Type().Field(i).Name, f.Kind())
		}
	}
}

// 2026-11-14, when the evidence ledger stopped importing the store (CI line's
// condition for the cut): the ledger reads a run through its own type, and a
// field added to it that the conversion forgets would reach the verdict as a
// zero value, silently. Every field of evidence.Run, and of its inputs, is
// what the store's run had.
func TestEvidenceRunCarriesEveryField(t *testing.T) {
	t.Parallel()
	var p store.PipelineRun
	fill(t, reflect.ValueOf(&p).Elem())
	got := reflect.ValueOf(EvidenceRun(p))
	for i := 0; i < got.NumField(); i++ {
		name := got.Type().Field(i).Name
		want := reflect.ValueOf(p).FieldByName(name)
		if !want.IsValid() {
			t.Errorf("evidence.Run.%s has no field of that name in store.PipelineRun; say in the conversion where it comes from and in this test how it is checked", name)
			continue
		}
		if got.Field(i).IsZero() {
			t.Errorf("evidence.Run.%s is zero after the conversion: EvidenceRun does not carry it", name)
			continue
		}
		if name == "Inputs" {
			in := got.Field(i).Index(0)
			for j := 0; j < in.NumField(); j++ {
				if w := want.Index(0).FieldByName(in.Type().Field(j).Name); !w.IsValid() || in.Field(j).Interface() != w.Interface() {
					t.Errorf("evidence.RepoAt.%s is not the store's after the conversion", in.Type().Field(j).Name)
				}
			}
			continue
		}
		if !reflect.DeepEqual(got.Field(i).Interface(), want.Interface()) {
			t.Errorf("evidence.Run.%s = %v, the store's run has %v", name, got.Field(i).Interface(), want.Interface())
		}
	}
}

// The evidence ledger writes the daily full CI's notes to the controller's
// stream under a constant of its own, since it no longer imports the store.
func TestEvidenceWritesToTheControllerStream(t *testing.T) {
	t.Parallel()
	if evidence.ControllerStream != store.ControllerStream {
		t.Fatalf("evidence.ControllerStream is %q, the store's is %q", evidence.ControllerStream, store.ControllerStream)
	}
}
