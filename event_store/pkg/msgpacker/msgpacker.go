package msgpacker

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/klauspost/compress/zstd"
	"github.com/vmihailenco/msgpack/v5"
)

const (
	// DefaultThreshold is the size (10KB) below which data is not compressed.
	DefaultThreshold = 10 * 1024

	// Payload markers (matching Ruby MessagePacker::WithCompression).
	markerRaw  byte = 0
	markerZstd byte = 1
)

// CompressionMode defines the compression algorithm to use.
type CompressionMode int

const (
	// ModeRaw disables compression.
	ModeRaw CompressionMode = iota
	// ModeZstd uses Zstd compression.
	ModeZstd
)

var (
	ErrEmptyData = fmt.Errorf("msgpacker: empty data")
	ErrUnknown   = fmt.Errorf("msgpacker: unknown marker")
)

// Options defines custom configuration for the Packer.
type Options struct {
	// Threshold is the minimum size in bytes to trigger compression. Default: 10KB.
	Threshold int

	// ZstdLevel configures the compression strength for ModeZstd. Default: zstd.SpeedDefault.
	ZstdLevel zstd.EncoderLevel
}

// Packer handles efficient serialization and compression.
// It is thread-safe and utilizes a sync.Pool for buffer reuse to minimize allocations.
//
// Format matches Ruby's MessagePacker::WithCompression:
//   - [marker(1b)][data...]
//
// Where marker is:
//   - 0: Raw (uncompressed msgpack data)
//   - 1: Zstd compressed msgpack data
type Packer struct {
	mode      CompressionMode
	threshold int
	zstdEnc   *zstd.Encoder
	zstdDec   *zstd.Decoder
	pool      sync.Pool
}

// New creates a configured Packer.
//
// Usage:
//
//	p := New(ModeRaw)
//	p := New(ModeZstd, Options{ZstdLevel: zstd.SpeedBestCompression})
func New(mode CompressionMode, opts ...Options) *Packer {
	// 1. Set Defaults
	config := Options{
		Threshold: DefaultThreshold,
		ZstdLevel: zstd.SpeedDefault,
	}

	// 2. Apply User Options
	if len(opts) > 0 {
		userOpts := opts[0]
		if userOpts.Threshold != 0 {
			config.Threshold = userOpts.Threshold
		}
		if userOpts.ZstdLevel != 0 {
			config.ZstdLevel = userOpts.ZstdLevel
		}
	}

	p := &Packer{
		mode:      mode,
		threshold: config.Threshold,
		pool: sync.Pool{
			New: func() interface{} {
				// Pre-allocate 4KB. It will grow if needed.
				return bytes.NewBuffer(make([]byte, 0, 4096))
			},
		},
	}

	// 3. Initialize Zstd (Safe for concurrency)
	var err error
	p.zstdEnc, err = zstd.NewWriter(nil, zstd.WithEncoderLevel(config.ZstdLevel))
	if err != nil {
		panic(fmt.Sprintf("msgpacker: zstd init failed: %v", err))
	}
	p.zstdDec, err = zstd.NewReader(nil)
	if err != nil {
		panic(fmt.Sprintf("msgpacker: zstd init failed: %v", err))
	}

	return p
}

// Pack serializes the object.
// If ModeRaw is set or data < threshold, it returns raw data.
// Otherwise, it compresses using Zstd.
//
// Format: [marker(1b)][data...]
func (p *Packer) Pack(v interface{}) ([]byte, error) {
	// 1. Serialize to intermediate pooled buffer
	buf := p.pool.Get().(*bytes.Buffer)
	defer func() {
		if buf.Cap() < 10*1024*1024 { // Sanity check for pool
			buf.Reset()
			p.pool.Put(buf)
		}
	}()
	buf.Reset()

	enc := msgpack.NewEncoder(buf)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("msgpack encode: %w", err)
	}

	rawBytes := buf.Bytes()

	// 2. Check if we should skip compression
	if p.mode == ModeRaw || len(rawBytes) < p.threshold {
		// Return Raw: [MarkerRaw][Data...]
		result := make([]byte, 1+len(rawBytes))
		result[0] = markerRaw
		copy(result[1:], rawBytes)
		return result, nil
	}

	// 3. Compress with Zstd
	// Format: [MarkerZstd][CompressedData...]
	result := make([]byte, 1, 1+len(rawBytes))
	result[0] = markerZstd
	return p.zstdEnc.EncodeAll(rawBytes, result), nil
}

// Unpack deserializes data, automatically handling compressed or raw formats.
// Format: [marker(1b)][data...]
func (p *Packer) Unpack(data []byte, v interface{}) error {
	if len(data) == 0 {
		return ErrEmptyData
	}

	marker := data[0]
	payload := data[1:]

	switch marker {
	case markerRaw:
		return msgpack.Unmarshal(payload, v)
	case markerZstd:
		uncompressed, err := p.zstdDec.DecodeAll(payload, nil)
		if err != nil {
			return fmt.Errorf("zstd decompress: %w", err)
		}
		return msgpack.Unmarshal(uncompressed, v)
	default:
		return ErrUnknown
	}
}
