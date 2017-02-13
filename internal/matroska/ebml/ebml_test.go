package ebml

import (
	"math/rand"
	"testing"
	"time"
)

func TestShortest(t *testing.T) {
	if b := shortest(0); len(b) != 0 {
		t.Fatalf("0 encoded to %#v, want %#v", b, []byte{})
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < 1e6; i++ {
		v1 := uint64(rng.Int63())>>31 | uint64(rng.Int63())<<32
		b := shortest(v1)
		var v2 uint64
		for i, c := range b {
			v2 |= uint64(c) << (uint(len(b)-i-1) * 8)
		}
		if v1 != v2 {
			t.Fatalf("%d encoded as %x which decodes to %d", v1, b, v2)
		}
	}
}
