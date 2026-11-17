//bifrost:owner foundation

package cluster

import (
	"encoding/json"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metrics "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	"reflect"
	"testing"
	"time"
)

func podRankingResult(fn func() []PodMetricView) (rows []PodMetricView, failure string) {
	defer func() {
		if p := recover(); p != nil {
			failure = fmt.Sprint(p)
		}
	}()
	return fn(), ""
}
func TestPodRankingProjectionMatchesOriginal(t *testing.T) {
	pod := func(ns, name string, cpu ...string) metrics.PodMetrics {
		p := metrics.PodMetrics{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}
		for _, c := range cpu {
			p.Containers = append(p.Containers, metrics.ContainerMetrics{Usage: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(c), corev1.ResourceMemory: resource.MustParse("1536Ki")}})
		}
		return p
	}
	sets := [][]metrics.PodMetrics{nil, {}, {pod("a", "empty")}, {pod("a", "one", "1m"), pod("b", "big", "1", "500m"), pod("a", "tie", "1m"), pod("a", "fraction", "0.0001", "0.0001")}}
	for _, pods := range sets {
		for _, filter := range []map[string]bool{nil, {}, {"a": true}, {"a": false, "b": true}} {
			for _, limit := range []int{-2, -1, 0, 1, 2, 20} {
				want, wp := podRankingResult(func() []PodMetricView { return baselineTopPodsByCPU(pods, filter, limit) })
				got, gp := podRankingResult(func() []PodMetricView { return topPodsByCPU(pods, filter, limit) })
				if !reflect.DeepEqual(want, got) || wp != gp {
					t.Fatalf("filter=%v limit=%d: original=%#v/%q current=%#v/%q", filter, limit, want, wp, got, gp)
				}
				w, _ := json.Marshal(want)
				g, _ := json.Marshal(got)
				if string(w) != string(g) {
					t.Fatal("JSON differs")
				}
			}
		}
	}
}
func TestEventProjectionMatchesOriginal(t *testing.T) {
	at := time.Date(2026, 11, 16, 1, 2, 3, 123000000, time.FixedZone("offset", 3600))
	for mask := 0; mask < 8; mask++ {
		e := corev1.Event{ObjectMeta: metav1.ObjectMeta{Namespace: "demo"}, Type: "Warning", Reason: "Reason", Message: "界\nmessage", Count: -1, InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "sample"}}
		if mask&1 != 0 {
			e.FirstTimestamp = metav1.NewTime(at)
		}
		if mask&2 != 0 {
			e.LastTimestamp = metav1.NewTime(at.Add(time.Hour))
		}
		if mask&4 != 0 {
			e.EventTime = metav1.NewMicroTime(at.Add(time.Minute))
		}
		want, got := baselineEventView(e), eventView(e)
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("mask %d", mask)
		}
		w, _ := json.Marshal(want)
		g, _ := json.Marshal(got)
		if string(w) != string(g) {
			t.Fatal("JSON differs")
		}
	}
	if !reflect.DeepEqual(baselineEventView(corev1.Event{}), eventView(corev1.Event{})) {
		t.Fatal("empty event differs")
	}
}
