// Package versionedread gives a read route a version (2026-11-18, the read
// routes of the bus's generation change, R03 and R05): the answer's hash is its
// ETag, and a caller that sends the version it holds gets 304 when nothing
// changed. The hash is of what the caller would receive, so it cannot say
// "unchanged" of an answer that changed.
package versionedread

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/readerseen"
)

// Reader is the mark a 304 leaves: a caller was spared the body.
const Reader = "versioned-reads"

type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *recorder) Header() http.Header { return r.header }
func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
}
func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(b)
}

// matches says whether an If-None-Match value names etag.
func matches(header, etag string) bool {
	for _, v := range strings.Split(header, ",") {
		v = strings.TrimSpace(v)
		if v == "*" || strings.TrimPrefix(v, "W/") == etag {
			return true
		}
	}
	return false
}

// Middleware versions the 200 answers of GET; anything else passes unchanged.
// The answer depends on who asks, so it is private and always revalidated.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		rec := &recorder{header: http.Header{}}
		next.ServeHTTP(rec, r)
		for k, v := range rec.header {
			w.Header()[k] = v
		}
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		if rec.status != http.StatusOK {
			w.WriteHeader(rec.status)
			_, _ = w.Write(rec.body.Bytes())
			return
		}
		sum := sha256.Sum256(rec.body.Bytes())
		etag := `"` + hex.EncodeToString(sum[:16]) + `"`
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "private, no-cache")
		w.Header().Add("Vary", "Authorization")
		if matches(r.Header.Get("If-None-Match"), etag) {
			readerseen.Saw(Reader)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(rec.body.Bytes())
	})
}
