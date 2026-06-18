package delivery

import "testing"

func TestStgAPIProbePath(t *testing.T) {
	if stgAPIProbePath("monitor") != "/status" {
		t.Fatal("monitor should use /status")
	}
	if stgAPIProbePath("massive") != "/health" {
		t.Fatal("massive should use /health")
	}
}
