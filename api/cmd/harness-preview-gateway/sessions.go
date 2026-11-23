package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/harnesssession"
)

const cookieName = "__Host-bifrost-harness"
const sessionLifetime = 20 * time.Minute
const maximumSessions = 1024

type session struct {
	host    string
	context context.Context
	cancel  context.CancelFunc
}

type sessions struct {
	parent string
	key    ed25519.PublicKey
	apps   map[string]string
	mu     sync.Mutex
	active map[string]session
	used   map[string]int64
}

func newSessions(parent string, key ed25519.PublicKey, apps map[string]string) *sessions {
	return &sessions{parent: "https://" + parent, key: key, apps: apps, active: make(map[string]session), used: make(map[string]int64)}
}

func (s *sessions) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Origin")
	if r.Method != http.MethodPost || r.Header.Get("Origin") != s.parent {
		http.Error(w, "Open this harness from Ops", http.StatusForbidden)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", s.parent)
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	switch r.URL.Path {
	case "/.bifrost/logout":
		s.mu.Lock()
		s.revoke(r)
		s.mu.Unlock()
		http.SetCookie(w, sessionCookie("", -1))
		w.WriteHeader(http.StatusNoContent)
	case "/.bifrost/session":
		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		if err := r.ParseForm(); err != nil || len(r.PostForm["grant"]) != 1 {
			http.Error(w, "Invalid Ops grant", http.StatusBadRequest)
			return
		}
		now := time.Now()
		claims, err := harnesssession.Verify(s.key, r.PostForm.Get("grant"), s.apps[r.Host], now)
		if err != nil {
			http.Error(w, "Invalid Ops grant", http.StatusForbidden)
			return
		}
		value := make([]byte, 32)
		if _, err := rand.Read(value); err != nil {
			http.Error(w, "Session unavailable", http.StatusServiceUnavailable)
			return
		}
		id := base64.RawURLEncoding.EncodeToString(value)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.expire(now)
		if _, reused := s.used[claims.Nonce]; reused {
			http.Error(w, "Expired or used Ops grant", http.StatusForbidden)
			return
		}
		s.revoke(r)
		if len(s.active) >= maximumSessions || len(s.used) >= maximumSessions {
			http.Error(w, "Session capacity reached", http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithDeadline(context.Background(), now.Add(sessionLifetime))
		s.active[id] = session{host: r.Host, context: ctx, cancel: cancel}
		s.used[claims.Nonce] = claims.Expires
		http.SetCookie(w, sessionCookie(id, int(sessionLifetime.Seconds())))
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

// Under the lock, discard only a cookie for this origin.
func (s *sessions) revoke(r *http.Request) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return
	}
	if active, ok := s.active[cookie.Value]; ok && active.host == r.Host {
		active.cancel()
		delete(s.active, cookie.Value)
	}
}

func (s *sessions) expire(now time.Time) {
	for id, active := range s.active {
		if active.context.Err() != nil {
			delete(s.active, id)
		}
	}
	for nonce, until := range s.used {
		if until <= now.Unix() {
			delete(s.used, nonce)
		}
	}
}

func sessionCookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{Name: cookieName, Value: value, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: maxAge}
}

func (s *sessions) lookup(r *http.Request) (session, bool) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return session{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	active, ok := s.active[cookie.Value]
	return active, ok && active.host == r.Host && active.context.Err() == nil
}

func stripSessionCookie(request *http.Request) {
	cookies := request.Cookies()
	request.Header.Del("Cookie")
	for _, cookie := range cookies {
		if cookie.Name != cookieName {
			request.AddCookie(cookie)
		}
	}
}
