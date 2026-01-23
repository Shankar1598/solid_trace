package msgpacker

import (
	"testing"
)

type TestStruct struct {
	Message  string            `msgpack:"message"`
	Count    int               `msgpack:"count"`
	Metadata map[string]string `msgpack:"metadata"`
	Enabled  bool              `msgpack:"enabled"`
}

func TestPackUnpack(t *testing.T) {
	original := TestStruct{
		Message: "test message",
		Count:   42,
		Metadata: map[string]string{
			"key1": "value1",
			"key2": "value2",
		},
		Enabled: true,
	}

	p := New(ModeRaw)

	// Pack the data (uncompressed)
	packed, err := p.Pack(original)
	if err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	// Unpack the data
	var unpacked TestStruct
	err = p.Unpack(packed, &unpacked)
	if err != nil {
		t.Fatalf("Unpack failed: %v", err)
	}

	// Verify the data matches
	if unpacked.Message != original.Message {
		t.Errorf("Message mismatch: got %s, want %s", unpacked.Message, original.Message)
	}
	if unpacked.Count != original.Count {
		t.Errorf("Count mismatch: got %d, want %d", unpacked.Count, original.Count)
	}
	if unpacked.Enabled != original.Enabled {
		t.Errorf("Enabled mismatch: got %v, want %v", unpacked.Enabled, original.Enabled)
	}
	if len(unpacked.Metadata) != len(original.Metadata) {
		t.Errorf("Metadata length mismatch: got %d, want %d", len(unpacked.Metadata), len(original.Metadata))
	}
}

func TestPackAlwaysUsesRawMarker(t *testing.T) {
	// Even large payloads should be uncompressed with Pack()
	largeData := make(map[string]string)
	for i := 0; i < 1000; i++ {
		largeData[string(rune(i))] = "some data that would normally compress"
	}

	large := TestStruct{
		Message:  "large payload",
		Count:    1000,
		Metadata: largeData,
	}

	p := New(ModeRaw)
	packed, err := p.Pack(large)
	if err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	// First byte should be the raw marker
	if packed[0] != markerRaw {
		t.Errorf("Expected raw marker (%d), got %d", markerRaw, packed[0])
	}
}

func TestPackAndCompressUsesZstdByDefault(t *testing.T) {
	// Create a large payload
	largeData := make(map[string]string)
	for i := 0; i < 1000; i++ {
		largeData[string(rune(i))] = "some data that will make this large enough for compression"
	}

	large := TestStruct{
		Message:  "large payload",
		Count:    1000,
		Metadata: largeData,
	}

	p := New(ModeZstd)

	// Pack with defaults (Zstd, Level 3, 10KB threshold)
	packed, err := p.Pack(large)
	if err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	// First byte should be the zstd marker
	if packed[0] != markerZstd {
		t.Errorf("Expected zstd marker (%d), got %d", markerZstd, packed[0])
	}

	// Unpack and verify
	var unpacked TestStruct
	err = p.Unpack(packed, &unpacked)
	if err != nil {
		t.Fatalf("Unpack failed: %v", err)
	}

	if unpacked.Message != large.Message {
		t.Errorf("Message mismatch after compression")
	}
}

func TestPackAndCompressRespectsThreshold(t *testing.T) {
	// Small payload below default threshold (10KB)
	small := TestStruct{
		Message: "small",
		Count:   1,
	}

	p := New(ModeZstd)
	packed, err := p.Pack(small)
	if err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	// Should be raw because it's small
	if packed[0] != markerRaw {
		t.Errorf("Expected raw marker for small payload, got %d", packed[0])
	}
}

func TestPackAndCompressWithCustomOptions(t *testing.T) {
	data := TestStruct{
		Message: "test",
		Count:   1,
	}

	// Force compression with low threshold
	opts := Options{
		Threshold: 1,
		ZstdLevel: 1, // Fastest
	}
	p := New(ModeZstd, opts)

	packed, err := p.Pack(data)
	if err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	if packed[0] != markerZstd {
		t.Errorf("Expected zstd marker with low threshold, got %d", packed[0])
	}

	// Unpack and verify
	var unpacked TestStruct
	err = p.Unpack(packed, &unpacked)
	if err != nil {
		t.Fatalf("Unpack failed: %v", err)
	}

	if unpacked.Message != data.Message {
		t.Error("Data mismatch after compression")
	}
}

func TestEmptyData(t *testing.T) {
	p := New(ModeZstd)
	var result TestStruct
	err := p.Unpack([]byte{}, &result)
	if err != ErrEmptyData {
		t.Errorf("Expected ErrEmptyData, got %v", err)
	}
}

func TestUnknownMarker(t *testing.T) {
	p := New(ModeZstd)
	// Create data with an invalid marker
	invalidData := []byte{99, 1, 2, 3, 4}

	var result TestStruct
	err := p.Unpack(invalidData, &result)
	if err == nil {
		t.Error("Expected error for unknown marker, got nil")
	}
}

func TestCompressionActuallyReducesSize(t *testing.T) {
	// Create highly compressible data
	repeatedData := make(map[string]string)
	for i := 0; i < 100; i++ {
		repeatedData[string(rune(i))] = "repeated text repeated text repeated text repeated text"
	}

	data := TestStruct{
		Message:  "compression test",
		Count:    100,
		Metadata: repeatedData,
	}

	p := New(ModeZstd)
	// Pack uncompressed to compare
	rawPacked, _ := p.Pack(data)

	// Pack compressed with explicit low threshold to ensure compression happens
	opts := Options{Threshold: 1024}
	p = New(ModeZstd, opts)
	compressedPacked, err := p.Pack(data)
	if err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	// Unpack to verify it works
	var unpacked TestStruct
	err = p.Unpack(compressedPacked, &unpacked)
	if err != nil {
		t.Fatalf("Unpack failed: %v", err)
	}

	// The compressed size should be significantly smaller
	t.Logf("Raw size: %d, Compressed size: %d", len(rawPacked), len(compressedPacked))
	if len(compressedPacked) >= len(rawPacked) {
		t.Error("Compression didn't reduce size")
	}

	// Verify data integrity
	if len(unpacked.Metadata) != len(data.Metadata) {
		t.Error("Metadata was corrupted during compression")
	}
}

func TestLZ4Compression(t *testing.T) {
	largeData := make(map[string]string)
	for i := 0; i < 100; i++ {
		largeData[string(rune(i))] = "test data for lz4 compression"
	}

	data := TestStruct{
		Message:  "lz4 test",
		Count:    100,
		Metadata: largeData,
		Enabled:  true,
	}

	// Pack with LZ4 - use a low threshold to ensure compression
	opts := Options{Threshold: 1024}
	p := New(ModeLZ4, opts)
	packed, err := p.Pack(data)
	if err != nil {
		t.Fatalf("Pack(LZ4) failed: %v", err)
	}

	// Verify LZ4 marker
	if packed[0] != markerLZ4 {
		t.Errorf("Expected LZ4 marker (%d), got %d", markerLZ4, packed[0])
	}

	// Unpack and verify
	var unpacked TestStruct
	err = p.Unpack(packed, &unpacked)
	if err != nil {
		t.Fatalf("Unpack failed: %v", err)
	}

	if unpacked.Message != data.Message {
		t.Errorf("Message mismatch: got %s, want %s", unpacked.Message, data.Message)
	}
}

// Benchmarks

func BenchmarkPackUncompressed(b *testing.B) {
	largeData := make(map[string]string)
	for i := 0; i < 1000; i++ {
		largeData[string(rune(i))] = "benchmark data"
	}
	data := TestStruct{
		Message:  "benchmark large",
		Count:    1000,
		Metadata: largeData,
	}

	p := New(ModeZstd)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.Pack(data)
	}
}

func BenchmarkPackZstdDefault(b *testing.B) {
	largeData := make(map[string]string)
	for i := 0; i < 1000; i++ {
		largeData[string(rune(i))] = "benchmark data"
	}
	data := TestStruct{
		Message:  "benchmark large",
		Count:    1000,
		Metadata: largeData,
	}

	p := New(ModeZstd)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.Pack(data)
	}
}

func BenchmarkPackLZ4(b *testing.B) {
	largeData := make(map[string]string)
	for i := 0; i < 1000; i++ {
		largeData[string(rune(i))] = "benchmark data"
	}
	data := TestStruct{
		Message:  "benchmark large",
		Count:    1000,
		Metadata: largeData,
	}

	p := New(ModeLZ4)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.Pack(data)
	}
}

func TestUnpackHandlesCompressedData(t *testing.T) {
	data := TestStruct{
		Message: "test",
		Count:   1,
	}

	// Force compression
	opts := Options{
		Threshold: 1,
		ZstdLevel: 1,
	}
	p := New(ModeZstd, opts)

	packed, err := p.Pack(data)
	if err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	if packed[0] == markerRaw {
		t.Fatalf("Expected compressed data")
	}

	// Unpack should NOT fail now because Unpack detects markers
	var result TestStruct
	err = p.Unpack(packed, &result)
	if err != nil {
		t.Errorf("Expected no error when unpacking compressed data, got %v", err)
	}
}
