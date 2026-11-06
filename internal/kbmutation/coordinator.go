package kbmutation

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Coordinator admits parallel knowledge operations and excludes workspace
// transfers. A deployment-wide gate deliberately also covers operations that
// span multiple KBs and writes which have not resolved a KB ID yet.
// Every server and worker must point at the same lock-capable volume.
type Coordinator struct{ BaseDir string }

const operationGate = "__all_knowledge_operations__"

type leaseKey struct{}
type lease struct {
	mu      sync.Mutex
	baseDir string
	refs    int
	unlock  func() error
}

// Begin holds a shared lease until the returned function runs. Nested calls
// retain the lease rather than repeatedly opening files. A detached operation
// must call Begin before its parent returns; a context alone owns no lease.
func (c *Coordinator) Begin(ctx context.Context) (context.Context, func(), error) {
	if c == nil {
		return ctx, func() {}, nil
	}
	if err := ctx.Err(); err != nil {
		return ctx, nil, err
	}
	if l, ok := ctx.Value(leaseKey{}).(*lease); ok && l.baseDir == c.BaseDir {
		l.mu.Lock()
		if l.refs > 0 {
			l.refs++
			l.mu.Unlock()
			return ctx, l.release(), nil
		}
		l.mu.Unlock()
	}
	unlock, err := Acquire(c.BaseDir, operationGate, false)
	if err != nil {
		return ctx, nil, err
	}
	l := &lease{baseDir: c.BaseDir, refs: 1, unlock: unlock}
	return context.WithValue(ctx, leaseKey{}, l), l.release(), nil
}

func (l *lease) release() func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.refs--
			if l.refs == 0 {
				_ = l.unlock()
			}
		})
	}
}

// Exclusive fails immediately if any admitted operation is still running.
// The caller keeps it through preflight, copy, commit and source cleanup.
func (c *Coordinator) Exclusive() (func() error, error) {
	if c == nil {
		return nil, ErrUnsupported
	}
	return Acquire(c.BaseDir, operationGate, true)
}

// WaitBegin parks background work without spending its retry budget on an
// administrative transfer. Request handlers use the non-blocking Begin.
func (c *Coordinator) WaitBegin(ctx context.Context) (context.Context, func(), error) {
	for {
		next, release, err := c.Begin(ctx)
		if !errors.Is(err, ErrBusy) {
			return next, release, err
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx, nil, ctx.Err()
		case <-timer.C:
		}
	}
}
