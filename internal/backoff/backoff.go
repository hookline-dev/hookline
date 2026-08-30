package backoff

import (
	"math/rand"
	"time"
)

type Config struct{ Base, Cap time.Duration }

func Next(attempt int, cfg Config, rnd *rand.Rand) time.Duration {
	if cfg.Base <= 0 || cfg.Cap <= 0 || rnd == nil {
		return 0
	}
	if attempt < 0 {
		attempt = 0
	}
	max := cfg.Base
	for i := 0; i < attempt && max < cfg.Cap; i++ {
		if max > cfg.Cap/2 {
			max = cfg.Cap
		} else {
			max *= 2
		}
	}
	if max > cfg.Cap {
		max = cfg.Cap
	}
	return time.Duration(rnd.Int63n(int64(max) + 1))
}
