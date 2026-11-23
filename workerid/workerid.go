// Package workerid hands out (datacenter, worker) ids via leases.
package workerid

import "context"

// Lease is exclusive ownership of one (dc, worker) pair.
type Lease interface {
	DC() uint8
	Worker() uint8
	// Done returns a receive-only channel. The lease implementation closes it
	// exactly once, when the lease is lost, so every reader wakes at the same
	// time and the generator stops issuing IDs. A nil channel means never lost.
	Done() <-chan struct{}
	Release(ctx context.Context) error
}

// Allocator hands out leases. It is an interface so etcd can replace Static.
type Allocator interface {
	Acquire(ctx context.Context) (Lease, error)
}

// static is a fixed id from flags, for a single node or local runs.
type static struct{ dc, worker uint8 }

// NewStatic returns an Allocator whose lease is never lost.
func NewStatic(dc, worker uint8) Allocator { return static{dc, worker} }

func (s static) Acquire(context.Context) (Lease, error) { return s, nil }

func (s static) DC() uint8     { return s.dc }
func (s static) Worker() uint8 { return s.worker }

// Done returns nil: a receive on a nil channel blocks forever, so it never fires.
func (static) Done() <-chan struct{}         { return nil }
func (static) Release(context.Context) error { return nil }
