package storage

import (
	"syscall"
	"testing"
)

func TestTheDefaultDiskLimitIsHalfTheDisk(t *testing.T) {
	dir := t.TempDir()
	var fs syscall.Statfs_t
	if err := syscall.Statfs(dir, &fs); err != nil {
		t.Fatalf("statfs: %v", err)
	}
	disk := int64(fs.Blocks) * int64(fs.Bsize)

	limit, err := NewLogDiskLimit(0, dir)
	if err != nil {
		t.Fatalf("NewLogDiskLimit: %v", err)
	}
	if limit.Bytes != disk/2 {
		t.Errorf("Expected the default limit to be half the %d-byte disk, got %d", disk, limit.Bytes)
	}

	limit, err = NewLogDiskLimit(800_000_000_000, dir)
	if err != nil || limit.Bytes != 800_000_000_000 {
		t.Errorf("Expected a configured limit to win, got %d, %v", limit.Bytes, err)
	}
}
