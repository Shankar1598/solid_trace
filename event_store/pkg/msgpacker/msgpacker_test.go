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
	// Even large payloads should be uncompressed with ModeRaw
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

func TestPackAndCompressUsesZstd(t *testing.T) {
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

func TestPackRespectsThreshold(t *testing.T) {
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

func TestPackWithCustomOptions(t *testing.T) {
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
	if err != ErrUnknown {
		t.Errorf("Expected ErrUnknown, got %v", err)
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

	// Pack uncompressed with ModeRaw
	pRaw := New(ModeRaw)
	rawPacked, _ := pRaw.Pack(data)

	// Pack compressed with explicit low threshold to ensure compression happens
	opts := Options{Threshold: 1024}
	pZstd := New(ModeZstd, opts)
	compressedPacked, err := pZstd.Pack(data)
	if err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	// Unpack to verify it works
	var unpacked TestStruct
	err = pZstd.Unpack(compressedPacked, &unpacked)
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

	// Unpack should work
	var result TestStruct
	err = p.Unpack(packed, &result)
	if err != nil {
		t.Errorf("Expected no error when unpacking compressed data, got %v", err)
	}
}

// Benchmarks

func BenchmarkPackRaw(b *testing.B) {
	largeData := make(map[string]string)
	for i := 0; i < 1000; i++ {
		largeData[string(rune(i))] = "benchmark data"
	}
	data := TestStruct{
		Message:  "benchmark large",
		Count:    1000,
		Metadata: largeData,
	}

	p := New(ModeRaw)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.Pack(data)
	}
}

func BenchmarkPackZstd(b *testing.B) {
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
