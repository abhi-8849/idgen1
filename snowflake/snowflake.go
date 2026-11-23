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

// NextID returns a new unique ID.
func (g *Generator) NextID(ctx context.Context) (uint64, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.clock.NowMs()
	if now == g.lastMs {
		g.seq++ // overflow past 4095 is handled in the hardening step
	} else {
		g.seq = 0
	}
	g.lastMs = now

	return uint64(now-g.epochMs)<<timeShift | g.prefix | g.seq, nil
}
