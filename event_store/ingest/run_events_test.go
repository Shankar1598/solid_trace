package ingest

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/solidtrace/event_store/ingest/normalise"
)

// The server puts each Log of the Dispatcher in its own range: a file
// Attribute that the sender sends, of any type, becomes the string file
// runs/<run id> or dispatcher, and is in no other map.
func TestRunEventFileIsAStringOfTheServer(t *testing.T) {
	receipt := normalise.Receipt{At: time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC), RetentionDays: 30}
	for _, file := range []string{`5`, `true`, `{"a":1}`, `"elsewhere"`} {
		for _, c := range []struct {
			name, body, want string
		}{
			{"run event", `{"message":"m","run.id":"a1b2","run.seq":1,"file":` + file + `}`, "runs/a1b2"},
			{"Dispatcher warning", `{"message":"m","file":` + file + `}`, "dispatcher"},
		} {
			var object map[string]json.RawMessage
			if err := json.Unmarshal([]byte(c.body), &object); err != nil {
				t.Fatal(err)
			}
			log, outcome := normaliseRunEvent(object, receipt)
			if outcome == normalise.Dropped {
				t.Fatalf("%s with file %s: dropped", c.name, file)
			}
			if got := log.Attributes.String["file"]; got != c.want {
				t.Errorf("%s with file %s: expected file %q, got %q", c.name, file, c.want, got)
			}
			_, number := log.Attributes.Number["file"]
			_, boolean := log.Attributes.Bool["file"]
			_, object2 := log.Attributes.JSON["file"]
			if number || boolean || object2 {
				t.Errorf("%s with file %s: file is also in another map: %+v", c.name, file, log.Attributes)
			}
		}
	}
}
