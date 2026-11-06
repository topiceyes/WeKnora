package kbmutation

import (
	"context"
	"errors"
	"testing"
)

func TestCoordinatorRetainsNestedAndDetachedWork(t *testing.T) {
	c := &Coordinator{BaseDir: t.TempDir()}
	ctx, parent, err := c.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, child, err := c.Begin(context.WithoutCancel(ctx))
	if err != nil {
		t.Fatal(err)
	}
	parent()
	parent() // releasing one call twice must not release the child's lease
	if _, err := c.Exclusive(); !errors.Is(err, ErrBusy) {
		t.Fatalf("detached work was not retained: %v", err)
	}
	child()
	unlock, err := c.Exclusive()
	if err != nil {
		t.Fatal(err)
	}
	// Reusing an old context cannot bypass an exclusive move after its lease
	// has ended. Context propagation alone is not proof of live ownership.
	if _, _, err := c.Begin(ctx); !errors.Is(err, ErrBusy) {
		t.Fatalf("expired context bypassed transfer: %v", err)
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	_, release, err := c.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestCoordinatorRejectsCanceledOperation(t *testing.T) {
	c := &Coordinator{BaseDir: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := c.Begin(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled admission: %v", err)
	}
	unlock, err := c.Exclusive()
	if err != nil {
		t.Fatal(err)
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
}
