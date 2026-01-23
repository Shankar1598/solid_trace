package msgpacker

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4/v4"
	"github.com/vmihailenco/msgpack/v5"
)

const (
	// DefaultThreshold is the size (10KB) below which data is not compressed.
	DefaultThreshold = 10 * 1024

	// Payload markers.
	markerRaw  byte = 0
	markerZstd byte = 1
	markerLZ4  byte = 2
)

// CompressionMode defines the compression algorithm to use.
type CompressionMode int

const (
	// ModeRaw (Default) disables compression.
	ModeRaw CompressionMode = iota
	// ModeZstd provides better compression ratio but higher CPU usage.
	ModeZstd
	// ModeLZ4 provides faster compression/decompression with lower ratio.
	ModeLZ4
)

var (
	ErrEmptyData   = errors.New("msgpacker: empty data")
	ErrUnknown     = errors.New("msgpacker: unknown marker")
	ErrCorruptData = errors.New("msgpacker: corrupt data header")
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
//	p := New(ModeLZ4)
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
		// Allow 0 threshold if user explicitly wants it?
		// For backward compat with previous logic, we check > 0.
		// But if they want to force compress everything, 0 is valid.
		// Let's assume the user knows what they are doing if they set it.
		// However, the struct defaults to 0 int value.
		// Let's keep logic: if userOpts.Threshold != 0, use it.
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
// Otherwise, it compresses using the configured mode.
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
		// Return Raw: [MarkerRaw] [Data...]
		result := make([]byte, 1+len(rawBytes))
		result[0] = markerRaw
		copy(result[1:], rawBytes)
		return result, nil
	}

	// 3. Compress
	return p.compress(rawBytes)
}

// Unpack deserializes data, automatically handling compressed or raw formats.
func (p *Packer) Unpack(data []byte, v interface{}) error {
	if len(data) == 0 {
		return ErrEmptyData
	}

	marker := data[0]

	// Fast Path: Raw
	if marker == markerRaw {
		return msgpack.Unmarshal(data[1:], v)
	}

	// Slow Path: Decompress
	return p.decompressAndUnmarshal(data, v)
}

func (p *Packer) compress(src []byte) ([]byte, error) {
	// Format: [Marker (1b)] [OriginalSize (Varint)] [CompressedData]

	headerBuf := [binary.MaxVarintLen64 + 1]byte{}
	var marker byte
	if p.mode == ModeLZ4 {
		marker = markerLZ4
	} else {
		marker = markerZstd
	}

	headerBuf[0] = marker
	headerLen := 1 + binary.PutUvarint(headerBuf[1:], uint64(len(src)))

	switch p.mode {
	case ModeLZ4:
		maxSz := lz4.CompressBlockBound(len(src))
		result := make([]byte, headerLen+maxSz)
		copy(result, headerBuf[:headerLen])

		n, err := lz4.CompressBlock(src, result[headerLen:], nil)
		if err != nil {
			return nil, fmt.Errorf("lz4 compress: %w", err)
		}
		return result[:headerLen+n], nil

	case ModeZstd:
		result := make([]byte, headerLen, headerLen+len(src))
		copy(result, headerBuf[:headerLen])
		return p.zstdEnc.EncodeAll(src, result), nil
	}

	return nil, nil
}

func (p *Packer) decompressAndUnmarshal(data []byte, v interface{}) error {
	// Read original size from varint header.
	originalSize, bytesRead := binary.Uvarint(data[1:])
	if bytesRead <= 0 {
		return ErrCorruptData
	}

	payload := data[1+bytesRead:]
	uncompressed := make([]byte, originalSize)
	var err error

	switch data[0] {
	case markerLZ4:
		_, err = lz4.UncompressBlock(payload, uncompressed)
	case markerZstd:
		uncompressed, err = p.zstdDec.DecodeAll(payload, uncompressed[:0])
	default:
		return ErrUnknown
	}

	if err != nil {
		return fmt.Errorf("decompress: %w", err)
	}

	if err := msgpack.Unmarshal(uncompressed, v); err != nil {
		return fmt.Errorf("msgpack unmarshal: %w", err)
	}

	return nil
}
