package snowflake_test

import (
	"context"
	"errors"
	"testing"

	"github.com/abhi-8849/idgen1/snowflake"
)

// clockFunc lets a test script exactly what the clock returns on each read.
type clockFunc func() int64

func (f clockFunc) NowMs() int64 { return f() }

var baseMs = snowflake.DefaultEpoch.UnixMilli() + 1000

func newGen(t *testing.T, cfg snowflake.Config, c snowflake.Clock) *snowflake.Generator {
	t.Helper()
	g, err := snowflake.New(cfg, c)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestIDsAreUniqueAndIncreasing(t *testing.T) {
	g := newGen(t, snowflake.Config{}, nil)
	var last uint64
	for i := 0; i < 10000; i++ {
		id, err := g.NextID(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if id <= last {
			t.Fatalf("id %d is not greater than previous %d", id, last)
		}
		last = id
	}
}

func TestDecodeRoundTrip(t *testing.T) {
	g := newGen(t, snowflake.Config{DC: 3, Worker: 7}, clockFunc(func() int64 { return baseMs }))
	g.NextID(context.Background()) // seq 0
	id, _ := g.NextID(context.Background())
	p := g.Decode(id)
	if p.DC != 3 || p.Worker != 7 || p.Seq != 1 || p.Time.UnixMilli() != baseMs {
		t.Fatalf("unexpected parts: %+v", p)
	}
}

func TestSequenceOverflowWaitsForNextMs(t *testing.T) {
	reads := 0
	// Frozen at baseMs for 4100 reads (4096 IDs plus a few polls), then the clock moves on.
	clock := clockFunc(func() int64 {
		reads++
		if reads <= 4100 {
			return baseMs
		}
		return baseMs + 1
	})
	g := newGen(t, snowflake.Config{}, clock)
	seen := map[uint64]bool{}
	var last uint64
	for i := 0; i < 4097; i++ { // the 4097th ID must come from the next millisecond
		id, err := g.NextID(context.Background())
		if err != nil || seen[id] {
			t.Fatalf("i=%d err=%v duplicate=%v", i, err, seen[id])
		}
		seen[id], last = true, id
	}
	if p := g.Decode(last); p.Time.UnixMilli() != baseMs+1 || p.Seq != 0 {
		t.Fatalf("expected next ms with seq 0, got %+v", p)
	}
}

func TestSmallBackwardStepWaitsAndStaysMonotonic(t *testing.T) {
	script := []int64{baseMs, baseMs - 2, baseMs - 1, baseMs} // second read goes back 2ms, then recovers
	i := 0
	clock := clockFunc(func() int64 {
		v := script[min(i, len(script)-1)]
		i++
		return v
	})
	g := newGen(t, snowflake.Config{}, clock)
	first, _ := g.NextID(context.Background())
	second, err := g.NextID(context.Background())
	if err != nil || second <= first {
		t.Fatalf("want increasing id after small step back, got %d then %d (err %v)", first, second, err)
	}
}

func TestBigBackwardStepIsRefused(t *testing.T) {
	now := baseMs
	g := newGen(t, snowflake.Config{}, clockFunc(func() int64 { return now }))
	g.NextID(context.Background())
	now = baseMs - 100
	if _, err := g.NextID(context.Background()); !errors.Is(err, snowflake.ErrClockBackwards) {
		t.Fatalf("want ErrClockBackwards, got %v", err)
	}
}

func TestLeaseLostIsRefused(t *testing.T) {
	lost := make(chan struct{})
	g := newGen(t, snowflake.Config{Lost: lost}, nil)
	if _, err := g.NextID(context.Background()); err != nil {
		t.Fatal(err)
	}
	close(lost)
	if _, err := g.NextID(context.Background()); !errors.Is(err, snowflake.ErrLeaseLost) {
		t.Fatalf("want ErrLeaseLost, got %v", err)
	}
}

func TestEpochExhausted(t *testing.T) {
	far := snowflake.DefaultEpoch.UnixMilli() + 1<<41
	g := newGen(t, snowflake.Config{}, clockFunc(func() int64 { return far }))
	if _, err := g.NextID(context.Background()); !errors.Is(err, snowflake.ErrEpochExhausted) {
		t.Fatalf("want ErrEpochExhausted, got %v", err)
	}
}

func TestInvalidConfig(t *testing.T) {
	if _, err := snowflake.New(snowflake.Config{DC: 32}, nil); !errors.Is(err, snowflake.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}
