package captured_test

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/solidtrace/event_store/captured"
)

// The cases of the Captured repositories match run here and in the
// solidtrace CLI (cli/capture/repository_cases_test.go): the two copies of
// Matches cannot drift.

const repositoryCasesFile = "../../cli/capture/testdata/captured-repository-cases.json"

type repositoryCases struct {
	Matches []struct {
		Entry      string `json:"entry"`
		Repository string `json:"repository"`
		Match      bool   `json:"match"`
	} `json:"matches"`
	Clones []struct {
		Name     string   `json:"name"`
		URL      string   `json:"url"`
		URLNames []string `json:"url_names"`
		Entries  []struct {
			Entry     string `json:"entry"`
			Telemetry bool   `json:"telemetry"`
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
		if got := captured.Matches(c.Entry, c.Repository); got != c.Match {
			t.Errorf("Matches(%q, %q) = %v, want %v", c.Entry, c.Repository, got, c.Match)
		}
	}
}

// Each clone's url gives its names, and each entry keeps the clone's
// telemetry as the case says.
func TestAnEntryKeepsTheTelemetryOfEachSharedClone(t *testing.T) {
	for _, clone := range readRepositoryCases(t).Clones {
		t.Run(clone.Name, func(t *testing.T) {
			if names := captured.URLNames(clone.URL); !slices.Equal(names, clone.URLNames) {
				t.Fatalf("URLNames(%q) = %q, want %q", clone.URL, names, clone.URLNames)
			}
			for _, e := range clone.Entries {
				filter := captured.NewFilter([]string{e.Entry})
				if got := filter.Keeps(clone.URL); got != e.Telemetry {
					t.Errorf("The filter of %q keeps %q: %v, want %v", e.Entry, clone.URL, got, e.Telemetry)
				}
			}
		})
	}
}

// A value that is not the web address of a repository has no name, so the
// filter keeps no record with it.
func TestAValueThatIsNotARepositoryURLHasNoName(t *testing.T) {
	for _, value := range []string{
		"",
		"github.com/acme/shop",
		"git@github.com:acme/shop.git",
		"ssh://git@github.com/acme/shop.git",
		"file:///srv/acme/shop.git",
		"https://github.com/acme",
		"https://github.com//shop",
		"https://github.com/acme/../shop",
		"https://github.com/~acme/shop",
		"https:///acme/shop",
		"https://github.com/%zz/shop",
	} {
		if names := captured.URLNames(value); names != nil {
			t.Errorf("URLNames(%q) = %q, want none", value, names)
		}
		if captured.NewFilter([]string{"github.com/acme/*"}).Keeps(value) {
			t.Errorf("The filter keeps %q", value)
		}
	}
}

// The port, a query and a fragment are not part of a name.
func TestAURLNameHasNoPortQueryOrFragment(t *testing.T) {
	want := []string{"git.example.com/team/app"}
	for _, value := range []string{
		"https://Git.Example.com:8443/team/app",
		"https://git.example.com/team/app.git?ref=main#readme",
		"http://git.example.com/team/app/",
	} {
		if names := captured.URLNames(value); !slices.Equal(names, want) {
			t.Errorf("URLNames(%q) = %q, want %q", value, names, want)
		}
	}
}

// An empty list keeps no record: the Agent Project's key then writes
// nothing.
func TestAFilterOfNoEntriesKeepsNoRecord(t *testing.T) {
	if captured.NewFilter(nil).Keeps("https://github.com/acme/shop") {
		t.Error("A filter of no entries keeps a record")
	}
}
