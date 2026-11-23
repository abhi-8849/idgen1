// Package snowflake generates 64-bit, unique, roughly time-sortable IDs.
//
// Layout: 1 sign bit (0) | 41 bits ms since epoch | 5 bits dc | 5 bits worker | 12 bits sequence.
package snowflake

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const (
	dcBits     = 5
	workerBits = 5
	seqBits    = 12

	workerShift = seqBits
	dcShift     = seqBits + workerBits
	timeShift   = seqBits + workerBits + dcBits
	timeBits    = 41
	seqMask     = 1<<seqBits - 1

	maxID = 1<<dcBits - 1 // 31, the same limit for dc and worker
)

// DefaultEpoch is the start of the 69-year timestamp range.
var DefaultEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// Config describes one generator. DC and Worker must be unique per running node.
type Config struct {
	Epoch           time.Time     // default DefaultEpoch
	DC, Worker      uint8         // each 0-31
	MaxBackwardWait time.Duration // default 5ms
}

// Generator issues IDs. It is safe for concurrent use.
type Generator struct {
	clock   Clock
	epochMs int64
	prefix  uint64 // dc and worker bits, fixed for the generator's life
	maxWait time.Duration
	mu      sync.Mutex // guards lastMs and seq
	lastMs  int64
	seq     uint64
}

// New validates cfg, fills defaults and returns a Generator. A nil clock means SystemClock.
func New(cfg Config, clock Clock) (*Generator, error) {
	if cfg.DC > maxID || cfg.Worker > maxID {
		return nil, fmt.Errorf("%w: dc and worker must be 0-%d", ErrInvalidConfig, maxID)
	}
	if cfg.Epoch.IsZero() {
		cfg.Epoch = DefaultEpoch
	}
	if cfg.MaxBackwardWait == 0 {
		cfg.MaxBackwardWait = 5 * time.Millisecond
	}
	if clock == nil {
		clock = SystemClock{}
	}
	return &Generator{
		clock:   clock,
		epochMs: cfg.Epoch.UnixMilli(),
		prefix:  uint64(cfg.DC)<<dcShift | uint64(cfg.Worker)<<workerShift,
		maxWait: cfg.MaxBackwardWait,
		lastMs:  -1,
	}, nil
}

// NextID returns a new unique ID. It may wait briefly for the clock, so it honours ctx.
func (g *Generator) NextID(ctx context.Context) (uint64, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	now, err := g.advance(ctx)
	if err != nil {
		return 0, err
	}
	ts := now - g.epochMs
	if ts >= 1<<timeBits {
		return 0, ErrEpochExhausted
	}
	g.lastMs = now
	return uint64(ts)<<timeShift | g.prefix | g.seq, nil
}

// advance picks the timestamp and sequence for the next ID. The caller holds g.mu.
func (g *Generator) advance(ctx context.Context) (int64, error) {
	now := g.clock.NowMs()

	if now < g.lastMs {
		// Never issue an older timestamp: ride out a small step back, refuse a big one.
		if gap := g.lastMs - now; time.Duration(gap)*time.Millisecond > g.maxWait {
			return 0, fmt.Errorf("%w: behind by %dms", ErrClockBackwards, gap)
		}
		var err error
		if now, err = g.waitUntil(ctx, g.lastMs); err != nil {
			return 0, err
		}
	}

	if now > g.lastMs {
		g.seq = 0
		return now, nil
	}

	g.seq = (g.seq + 1) & seqMask
	if g.seq != 0 {
		return now, nil
	}
	// All 4096 sequence numbers used in this ms: take the next ms.
	return g.waitUntil(ctx, now+1)
}

// waitUntil blocks until the clock reads at least ms, or ctx ends.
func (g *Generator) waitUntil(ctx context.Context, ms int64) (int64, error) {
	for {
		if now := g.clock.NowMs(); now >= ms {
			return now, nil
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
}
