package storage

import (
	"encoding/binary"
	"encoding/hex"
	"time"
)

const MaxUint64 = ^uint64(0)

// KeyForEvent generates a binary key for RocksDB.
// Format: ProjectID (4 bytes BE) + ReverseTimestamp (8 bytes BE) + EventUUID (16 bytes)
//
// Why reverse timestamp? So that scanning by prefix (ProjectID) returns
// events in newest-first order.
func KeyForEvent(projectID uint32, eventUUID string, timestamp time.Time) []byte {
	key := make([]byte, 28) // 4 + 8 + 16 = 28 bytes

	// 1. ProjectID as 4-byte big-endian
	binary.BigEndian.PutUint32(key[0:4], projectID)

	// 2. Reverse timestamp (microseconds since epoch, inverted)
	microSeconds := uint64(timestamp.UnixMicro())
	reverseTimestamp := MaxUint64 - microSeconds
	binary.BigEndian.PutUint64(key[4:12], reverseTimestamp)

	// 3. EventUUID as 16 raw bytes (decode from hex string)
	//    Event UUID comes as "8e06f9c623114e978329e37700b5f261" (32 hex chars)
	uuidBytes, _ := hex.DecodeString(eventUUID)
	copy(key[12:28], uuidBytes)

	return key
}
