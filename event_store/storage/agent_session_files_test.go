package storage

import (
	"bytes"
	"strings"
	"testing"
)

// checkAgentFileRows checks that rows cover unit from offset without gaps,
// that no row is longer than agentFileRowBytes, and that the rows joined
// are unit.
func checkAgentFileRows(t *testing.T, offset uint64, unit []byte, rows []agentFileRow) {
	t.Helper()
	next := offset
	var joined []byte
	for i, row := range rows {
		if row.offset != next {
			t.Fatalf("row %d starts at %d, want %d", i, row.offset, next)
		}
		if len(row.bytes) == 0 || len(row.bytes) > agentFileRowBytes {
			t.Fatalf("row %d has %d bytes, want 1 to %d", i, len(row.bytes), agentFileRowBytes)
		}
		next += uint64(len(row.bytes))
		joined = append(joined, row.bytes...)
	}
	if !bytes.Equal(joined, unit) {
		t.Fatalf("rows joined (%d bytes) are not the unit (%d bytes)", len(joined), len(unit))
	}
}

func TestAgentFileRowsEndAfterANewline(t *testing.T) {
	line := `{"type":"assistant","message":"` + strings.Repeat("x", 90) + `"}` + "\n"
	var unit []byte
	for len(unit) < 3*agentFileRowBytes {
		unit = append(unit, line...)
	}
	unit = append(unit, `{"type":"partial"`...) // the last line has no newline yet

	const offset = 4096
	rows := agentFileRows(offset, unit)
	if len(rows) < 3 {
		t.Fatalf("got %d rows, want at least 3", len(rows))
	}
	checkAgentFileRows(t, offset, unit, rows)
	for i, row := range rows[:len(rows)-1] {
		if row.bytes[len(row.bytes)-1] != '\n' {
			t.Fatalf("row %d does not end after a newline", i)
		}
	}
}

func TestAgentFileRowsCutALineLongerThanARow(t *testing.T) {
	long := strings.Repeat("y", agentFileRowBytes+500) + "\n"
	unit := []byte(long + "{\"short\":1}\n")

	rows := agentFileRows(0, unit)
	checkAgentFileRows(t, 0, unit, rows)
	if len(rows[0].bytes) != agentFileRowBytes {
		t.Fatalf("first row has %d bytes, want the row size %d", len(rows[0].bytes), agentFileRowBytes)
	}
}

func TestAgentFileRowsKeepASmallUnitInOneRow(t *testing.T) {
	unit := []byte("{\"a\":1}\n{\"b\":2}")
	rows := agentFileRows(7, unit)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	checkAgentFileRows(t, 7, unit, rows)
}
