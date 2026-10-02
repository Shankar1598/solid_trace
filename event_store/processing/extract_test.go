package processing

import (
	"testing"
	"time"
)

func TestExtractTimestamp(t *testing.T) {
	receivedAt := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		timestamp interface{}
		want      time.Time
	}{
		{"unix seconds", 1700000000.5, time.UnixMilli(1700000000500)},
		{"RFC 3339 with Z", "2023-11-14T22:13:20.123456Z", time.Date(2023, 11, 14, 22, 13, 20, 123456000, time.UTC)},
		{"RFC 3339 with offset", "2023-11-14T22:13:20+05:30", time.Date(2023, 11, 14, 16, 43, 20, 0, time.UTC)},
		{"no offset means UTC", "2023-11-14T22:13:20.5", time.Date(2023, 11, 14, 22, 13, 20, 500000000, time.UTC)},
		{"unparseable falls back to receive time", "not a time", receivedAt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTimestamp(map[string]interface{}{"timestamp": tt.timestamp}, receivedAt)
			if !got.Equal(tt.want) {
				t.Errorf("expected %v, got %v", tt.want, got)
			}
		})
	}
}
