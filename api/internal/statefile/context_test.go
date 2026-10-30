package statefile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Serial: switches the process-wide backend.
func TestContextBudgetReachesBackendReadAndUpdate(t *testing.T) {
	root := t.TempDir()
	Use(waitingBackend{}, root)
	defer Use(nil, "")
	for _, update := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		started := time.Now()
		var err error
		if update {
			err = UpdateContext(ctx, filepath.Join(root, "state"), func([]byte) ([]byte, error) { t.Fatal("cancelled request mutated state"); return nil, nil })
		} else {
			_, err = ReadFileContext(ctx, filepath.Join(root, "state"))
		}
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > time.Second {
			t.Fatalf("caller budget lost: %v", err)
		}
	}
}

type waitingBackend struct{}

func (waitingBackend) Read(ctx context.Context, _ string) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (waitingBackend) Write(ctx context.Context, _ string, _ []byte) error {
	<-ctx.Done()
	return ctx.Err()
}
func (waitingBackend) Update(ctx context.Context, _ string, _ func([]byte) ([]byte, error)) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestContextBudgetCancelsFileLockWait(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state")
	lock, err := os.Create(path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = UpdateContext(ctx, path, func([]byte) ([]byte, error) { t.Fatal("lock was not held"); return nil, nil })
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > time.Second {
		t.Fatalf("lock wait ignored budget: %v", err)
	}
}
