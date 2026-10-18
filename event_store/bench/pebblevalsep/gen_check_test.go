package pebblevalsep

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// TestGen checks the payloads are valid JSON and prints their size spread and
// how well they compress, so the mix can be compared with real Events.
func TestGen(t *testing.T) {
	envOrSkip(t)
	payloads := newPayloads(20_000, 1)
	sizes := make([]int, len(payloads))
	var total int
	for i, p := range payloads {
		if !json.Valid(p) {
			t.Fatalf("payload %d is not JSON: %s", i, p)
		}
		sizes[i] = len(p)
		total += len(p)
	}
	slices.Sort(sizes)
	q := func(f float64) int { return sizes[int(f*float64(len(sizes)-1))] }
	var over512, over2k int
	for _, s := range sizes {
		if s >= 512 {
			over512++
		}
		if s >= 2048 {
			over2k++
		}
	}
	t.Logf("mean %d B, min %d, p10 %d, p50 %d, p90 %d, p99 %d, max %d; ≥512 B %.0f%%, ≥2 KB %.0f%%",
		total/len(sizes), sizes[0], q(0.1), q(0.5), q(0.9), q(0.99), sizes[len(sizes)-1],
		100*float64(over512)/float64(len(sizes)), 100*float64(over2k)/float64(len(sizes)))

	// Compress 32 KB runs of payloads, about one Pebble block's worth.
	enc, _ := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
	var raw, packed int
	var block []byte
	for _, p := range payloads {
		block = append(block, p...)
		if len(block) >= 32<<10 {
			raw += len(block)
			packed += len(enc.EncodeAll(block, nil))
			block = block[:0]
		}
	}
	t.Logf("zstd (fastest) on 32 KB blocks: %.2fx", float64(raw)/float64(packed))
}
