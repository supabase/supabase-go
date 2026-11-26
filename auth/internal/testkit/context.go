package testkit

import (
	"context"
	"sync"
	"testing"
	"time"
)

// WaitObservingContext is a [context.Context] that observes the first time its
// Done channel is asked for, which a call that blocks on the context does as
// it starts to wait. Construct with [NewWaitObservingContext].
type WaitObservingContext struct {
	context.Context
	once    sync.Once
	waiting chan struct{}
}

// NewWaitObservingContext returns a [WaitObservingContext] whose values,
// deadline and cancellation are parent's.
func NewWaitObservingContext(parent context.Context) *WaitObservingContext {
	return &WaitObservingContext{Context: parent, waiting: make(chan struct{})}
}

// Done returns parent's Done channel, observing that it was asked for.
func (c *WaitObservingContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

// AwaitWaiting returns once a call has asked for ctx's Done channel, and fails
// the test if none has within five seconds.
func AwaitWaiting(t *testing.T, ctx *WaitObservingContext) {
	t.Helper()
	select {
	case <-ctx.waiting:
	case <-time.After(5 * time.Second):
		t.Fatal("no call waited on the context within five seconds")
	}
}
