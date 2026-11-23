// Package etcd is a workerid allocator backed by etcd leases.
package etcd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/abhi-8849/idgen1/workerid"
)

const (
	leaseTTL   = 10              // seconds etcd keeps our key after the last keep-alive
	fenceAfter = 7 * time.Second // TTL minus a 3s margin: stop issuing before etcd can free the id
	maxWorker  = 31
	setupTime  = 5 * time.Second
)

// TODO: add an end-to-end test against a real etcd: two nodes get different worker ids, and Done closes about 7s after etcd stops.

// ErrNoFreeWorker means all 32 worker ids in this datacenter are taken.
var ErrNoFreeWorker = errors.New("etcd: no free worker id")

// Allocator claims a free worker id in one datacenter.
type Allocator struct {
	cli *clientv3.Client
	dc  uint8
}

var _ workerid.Allocator = (*Allocator)(nil)

// New connects to etcd. The client is closed by the lease's Release.
func New(endpoints []string, dc uint8) (*Allocator, error) {
	cli, err := clientv3.New(clientv3.Config{Endpoints: endpoints, DialTimeout: setupTime})
	if err != nil {
		return nil, fmt.Errorf("etcd: connect: %w", err)
	}
	return &Allocator{cli: cli, dc: dc}, nil
}

// Acquire grants a lease, claims the first free worker id and starts keeping it alive.
func (a *Allocator) Acquire(ctx context.Context) (workerid.Lease, error) {
	setupCtx, cancel := context.WithTimeout(ctx, setupTime)
	defer cancel()

	grant, err := a.cli.Grant(setupCtx, leaseTTL)
	if err != nil {
		return nil, fmt.Errorf("etcd: grant lease: %w", err)
	}
	worker, err := a.claim(setupCtx, grant.ID)
	if err != nil {
		a.cli.Revoke(context.Background(), grant.ID) // give the unused lease back
		return nil, err
	}

	// The keep-alive must outlive Acquire's ctx, so it gets its own context.
	// Release cancels it, which stops the keep-alive stream and our goroutine.
	kaCtx, stop := context.WithCancel(context.Background())
	ka, err := a.cli.KeepAlive(kaCtx, grant.ID)
	if err != nil {
		stop()
		return nil, fmt.Errorf("etcd: keep alive: %w", err)
	}
	l := &lease{cli: a.cli, id: grant.ID, dc: a.dc, worker: worker, stop: stop, done: make(chan struct{})}
	go l.watch(kaCtx, ka)
	return l, nil
}

// claim tries worker ids 0..31 and keeps the first one it can create.
func (a *Allocator) claim(ctx context.Context, id clientv3.LeaseID) (uint8, error) {
	host, _ := os.Hostname() // only a debugging hint stored in the key
	for w := 0; w <= maxWorker; w++ {
		key := fmt.Sprintf("/idgen/workers/%d/%d", a.dc, w)
		// Create only if absent: CreateRevision 0 means the key does not exist.
		// The transaction is atomic, so two nodes can never both win the same id.
		resp, err := a.cli.Txn(ctx).
			If(clientv3.Compare(clientv3.CreateRevision(key), "=", 0)).
			Then(clientv3.OpPut(key, host, clientv3.WithLease(id))).
			Commit()
		if err != nil {
			return 0, fmt.Errorf("etcd: claim %s: %w", key, err)
		}
		if resp.Succeeded {
			return uint8(w), nil
		}
	}
	return 0, ErrNoFreeWorker
}

// lease is a claimed worker id that is kept alive in the background.
type lease struct {
	cli    *clientv3.Client
	id     clientv3.LeaseID
	dc     uint8
	worker uint8
	stop   context.CancelFunc // cancels the keep-alive stream and wakes watch
	// done is closed by watch (its only closer) when the lease is lost or released.
	done chan struct{}
}

func (l *lease) DC() uint8             { return l.dc }
func (l *lease) Worker() uint8         { return l.worker }
func (l *lease) Done() <-chan struct{} { return l.done }

// watch runs in its own goroutine, started by Acquire. It reads keep-alive replies and
// closes done when none arrives for fenceAfter, so the generator stops (self-fencing)
// before etcd expires the key and another node can take the same id.
func (l *lease) watch(ctx context.Context, ka <-chan *clientv3.LeaseKeepAliveResponse) {
	defer close(l.done) // runs on every exit path; wakes everyone waiting on Done()
	timer := time.NewTimer(fenceAfter)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-ka: // the etcd client sends here after each successful keep-alive
			if !ok { // the client closed the stream: lease expired or connection gave up
				return
			}
			if !timer.Stop() {
				<-timer.C // drain so Reset starts from a clean timer
			}
			timer.Reset(fenceAfter)
		case <-timer.C: // silence for 7s: assume we are cut off from etcd
			return
		case <-ctx.Done(): // Release was called
			return
		}
	}
}

// Release frees the worker id at once by revoking the lease, then closes the client.
func (l *lease) Release(ctx context.Context) error {
	_, err := l.cli.Revoke(ctx, l.id) // deletes the key, so another node can claim it now
	l.stop()                          // stops the keep-alive stream and the watch goroutine
	l.cli.Close()
	return err
}
