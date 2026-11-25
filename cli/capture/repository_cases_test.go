package capture_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/solidtrace/cli/capture"
)

// The cases of the Captured repositories match run here and in EventStore
// (event_store/captured/captured_test.go): the two copies of Matches
// cannot drift.

const repositoryCasesFile = "testdata/captured-repository-cases.json"

type repositoryCases struct {
	Matches []struct {
		Entry      string `json:"entry"`
		Repository string `json:"repository"`
		Match      bool   `json:"match"`
	} `json:"matches"`
	Clones []struct {
		Name       string `json:"name"`
		Remote     string `json:"remote"`
		Repository string `json:"repository"`
		Entries    []struct {
			Entry    string `json:"entry"`
			Sessions bool   `json:"sessions"`
		} `json:"entries"`
	} `json:"clones"`
}

func readRepositoryCases(t *testing.T) repositoryCases {
	t.Helper()
	data, err := os.ReadFile(repositoryCasesFile)
	if err != nil {
		t.Fatal(err)
	}
	var cases repositoryCases
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("%s: %v", repositoryCasesFile, err)
	}
	if len(cases.Matches) == 0 || len(cases.Clones) == 0 {
		t.Fatalf("%s has no cases", repositoryCasesFile)
	}
	return cases
}

func TestMatchesGivesTheResultOfEachSharedCase(t *testing.T) {
	for _, c := range readRepositoryCases(t).Matches {
		if got := capture.Matches(c.Entry, c.Repository); got != c.Match {
			t.Errorf("Matches(%q, %q) = %v, want %v", c.Entry, c.Repository, got, c.Match)
		}
	}
}

// Each clone's remote gives its repository, and each entry captures the
// clone's Agent sessions as the case says.
func TestAnEntryCapturesTheAgentSessionsOfEachSharedClone(t *testing.T) {
	for _, clone := range readRepositoryCases(t).Clones {
		t.Run(clone.Name, func(t *testing.T) {
			repository, ok := capture.Normalise(clone.Remote)
			if !ok || repository != clone.Repository {
				t.Fatalf("Normalise(%q) = %q, %v; want %q", clone.Remote, repository, ok, clone.Repository)
			}
			for _, e := range clone.Entries {
				if got := capture.Matches(e.Entry, repository); got != e.Sessions {
					t.Errorf("Matches(%q, %q) = %v, want %v", e.Entry, repository, got, e.Sessions)
				}
			}
		})
	}
}
