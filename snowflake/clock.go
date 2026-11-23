package snowflake

import "time"

// Clock is an interface so tests can later inject a fake time source.
type Clock interface {
	// NowMs returns Unix time in milliseconds.
	NowMs() int64
}

// SystemClock reads the wall clock.
type SystemClock struct{}

// NowMs returns the current Unix time in milliseconds.
func (SystemClock) NowMs() int64 { return time.Now().UnixMilli() }
