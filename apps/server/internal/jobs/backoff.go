package jobs

import (
	"math/rand/v2"
	"time"
)

// Default pauses between the attempts at a failed job: five minutes, doubling up to six hours.
const (
	defaultRetryBase = 5 * time.Minute
	defaultRetryMax  = 6 * time.Hour
	// retryJitter is the share of a pause that is randomly cut off, so that the jobs that failed together
	// (a rate limit, a network outage) do not all come back at the same moment.
	retryJitter = 0.2
)

// Backoff is the pause before the next attempt at a job that keeps failing: Base after the first failure,
// twice as long after each one, never more than Max. Each pause is shortened by up to Jitter of itself, so
// Max really is the longest one.
type Backoff struct {
	Base, Max time.Duration
	// Jitter is the largest share of a pause that is cut off, between 0 and 1.
	Jitter float64
	// Random replaces the source of the jitter, a number in [0, 1) (tests).
	Random func() float64
}

// newBackoff builds the Backoff of a queue; a zero base or ceiling takes the default.
func newBackoff(base, ceiling time.Duration) *Backoff {
	return &Backoff{Base: positiveDuration(base, defaultRetryBase), Max: positiveDuration(ceiling, defaultRetryMax), Jitter: retryJitter}
}

// Delay is the pause after the failures-th failure (the first is 1).
func (b Backoff) Delay(failures int) time.Duration {
	delay := b.Base
	for i := 1; i < failures && delay < b.Max; i++ {
		delay *= 2
	}
	delay = min(delay, b.Max)
	random := b.Random
	if random == nil {
		random = rand.Float64
	}
	return delay - time.Duration(float64(delay)*b.Jitter*random())
}
