package hub

import (
	"math/rand/v2"
	"net/http"
	"time"
)

// RetriableStatus reports whether an HTTP status code warrants a retry. Server
// errors (5xx) and rate limiting (429) are transient; client errors such as
// 404, 401, 403, and 400 are terminal because retrying cannot change them.
func RetriableStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= 500
}

// RetryWaits normalizes backoff bounds, applying defaults of 1s and 5m and
// ensuring maxWait is not below minWait.
func RetryWaits(minWait, maxWait time.Duration) (time.Duration, time.Duration) {
	if minWait <= 0 {
		minWait = time.Second
	}
	if maxWait <= 0 {
		maxWait = 5 * time.Minute
	}
	if maxWait < minWait {
		maxWait = minWait
	}
	return minWait, maxWait
}

// RetryDelay returns a randomized backoff for the given zero-based retry
// attempt. The ceiling grows exponentially from minWait and is capped at
// maxWait; the returned wait is jittered within [ceiling/2, ceiling] (equal
// jitter) so many clients retrying a failed server spread out instead of
// thundering. During a sustained outage the wait settles between half and all
// of maxWait. Bounds should already be normalized via RetryWaits.
func RetryDelay(attempt int, minWait, maxWait time.Duration) time.Duration {
	minWait, maxWait = RetryWaits(minWait, maxWait)
	ceiling := minWait
	for i := 0; i < attempt; i++ {
		if ceiling >= maxWait {
			ceiling = maxWait
			break
		}
		ceiling *= 2
		if ceiling <= 0 || ceiling >= maxWait { // cap and guard against overflow
			ceiling = maxWait
			break
		}
	}
	half := ceiling / 2
	if half <= 0 {
		return ceiling
	}
	return half + time.Duration(rand.Int64N(int64(half)+1))
}
