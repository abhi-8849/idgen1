package snowflake

import "errors"

// Sentinel errors. Callers match them with errors.Is.
var (
	ErrClockBackwards = errors.New("snowflake: clock moved backwards")
	ErrEpochExhausted = errors.New("snowflake: timestamp no longer fits in 41 bits")
	ErrLeaseLost      = errors.New("snowflake: worker id lease lost")
	ErrInvalidConfig  = errors.New("snowflake: invalid config")
)
