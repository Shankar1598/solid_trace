package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// script writes an executable shell script with body, as a fake open or
// xdg-open.
func script(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "xdg-open")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTheOpenerFailsWhenTheProgramExitsWithAnError(t *testing.T) {
	// xdg-open exits 3 when no program can open the URL.
	err := opener(script(t, "exit 3"), 5*time.Second)("https://console.example.com/oauth/authorize")
	if err == nil {
		t.Fatal("no error: the CLI waits for a redirect that does not come")
	}
}

func TestTheOpenerPassesWhenTheProgramExitsWithNoError(t *testing.T) {
	if err := opener(script(t, "exit 0"), 5*time.Second)("https://console.example.com"); err != nil {
		t.Fatal(err)
	}
}

func TestTheOpenerPassesWhenTheProgramIsStillRunning(t *testing.T) {
	// xdg-open can wait until the browser closes.
	start := time.Now()
	if err := opener(script(t, "sleep 2"), 100*time.Millisecond)("https://console.example.com"); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("the opener waited for the program to exit")
	}
}
