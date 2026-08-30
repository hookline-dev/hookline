package backoff

import (
	"math/rand"
	"testing"
	"time"
)

func TestNext(t *testing.T) {
	// #nosec G404 -- deterministic pseudorandom sources are required by this test.
	a := rand.New(rand.NewSource(1))
	// #nosec G404 -- deterministic pseudorandom sources are required by this test.
	b := rand.New(rand.NewSource(1))
	cfg := Config{time.Second, time.Hour}
	for i := -1; i < 30; i++ {
		x, y := Next(i, cfg, a), Next(i, cfg, b)
		if x != y || x < 0 || x > time.Hour {
			t.Fatalf("%d %v %v", i, x, y)
		}
	}
	if Next(1, Config{}, a) != 0 || Next(1, cfg, nil) != 0 {
		t.Fatal("edge")
	}
	if got := Next(100, cfg, a); got < 0 || got > cfg.Cap {
		t.Fatalf("overflow attempt produced %v", got)
	}
	if got := Next(0, Config{Base: 2 * time.Hour, Cap: time.Hour}, a); got < 0 || got > time.Hour {
		t.Fatalf("base greater than cap produced %v", got)
	}
}
