package agentthreads

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sync"

	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
)

// Store is the statefile key that holds State. An empty path keeps it in
// memory (tests, and a process without a data directory).
type Store struct {
	path string
	mu   sync.Mutex
	mem  State
}

func NewStore(path string) *Store { return &Store{path: path} }

func decodeState(raw []byte) (State, error) {
	st := State{Threads: map[string]Thread{}, Hosts: map[string]Host{}}
	if len(raw) == 0 {
		return st, nil
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return State{}, fmt.Errorf("agent threads state: %w", err)
	}
	if st.Threads == nil {
		st.Threads = map[string]Thread{}
	}
	if st.Hosts == nil {
		st.Hosts = map[string]Host{}
	}
	return st, nil
}

func clone(st State) State {
	out := State{
		Threads:           make(map[string]Thread, len(st.Threads)),
		Hosts:             make(map[string]Host, len(st.Hosts)),
		ThreadsRefused:    st.ThreadsRefused,
		LastThreadRefusal: st.LastThreadRefusal,
	}
	for k, t := range st.Threads {
		if t.PriorTurns != nil {
			t.PriorTurns = append([]string(nil), t.PriorTurns...)
		}
		if t.SupersededKeys != nil {
			t.SupersededKeys = append([]string(nil), t.SupersededKeys...)
		}
		out.Threads[k] = t
	}
	for k, h := range st.Hosts {
		h.Vendors = cloneVendors(h.Vendors)
		out.Hosts[k] = h
	}
	return out
}

// Load reads the current state. A missing key is an empty state.
func (s *Store) Load() (State, error) {
	if s.path == "" {
		s.mu.Lock()
		defer s.mu.Unlock()
		return clone(s.mem), nil
	}
	raw, err := statefile.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return State{Threads: map[string]Thread{}, Hosts: map[string]Host{}}, nil
		}
		return State{}, err
	}
	return decodeState(raw)
}

// Update applies mutate to the latest state and writes it back. On a write
// conflict the backend re-reads and runs mutate again, so the API pod folding
// in events and the workers pod marking a push never lose each other's change.
func (s *Store) Update(mutate func(*State) error) error {
	if s.path == "" {
		s.mu.Lock()
		defer s.mu.Unlock()
		st := clone(s.mem)
		if err := mutate(&st); err != nil {
			return err
		}
		s.mem = st
		return nil
	}
	return statefile.Update(s.path, func(old []byte) ([]byte, error) {
		st, err := decodeState(old)
		if err != nil {
			return nil, err
		}
		if err := mutate(&st); err != nil {
			return nil, err
		}
		return json.Marshal(st)
	})
}
