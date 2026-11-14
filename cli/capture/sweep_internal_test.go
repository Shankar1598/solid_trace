package capture

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"testing"

	"github.com/solidtrace/cli/agentapi"
)

// The sweep tests in ./command prove the statuses that the offsets call and
// an upload give to the sweep. A 409 or a 400 of an upload stays in the
// upload (the path is pending), so those tests cannot reach this rule for
// them: this table holds the full rule.
func TestStopsTheSweep(t *testing.T) {
	for _, tc := range []struct {
		err   error
		stops bool
	}{
		{&agentapi.Error{Status: http.StatusBadRequest}, false},
		{&agentapi.Error{Status: http.StatusForbidden}, false},
		{&agentapi.Error{Status: http.StatusNotFound}, false},
		{&agentapi.Error{Status: http.StatusConflict}, false},
		{&agentapi.Error{Status: http.StatusUnauthorized}, true},
		{&agentapi.Error{Status: http.StatusUpgradeRequired}, true},
		{&agentapi.Error{Status: http.StatusInternalServerError}, true},
		{&agentapi.Error{Status: http.StatusServiceUnavailable}, true},
		{&fs.PathError{Op: "open", Path: "t1.txt", Err: fs.ErrPermission}, false},
		{errors.New("dial tcp: connection refused"), true},
	} {
		err := fmt.Errorf("upload x: %w", tc.err)
		if got := stopsTheSweep(err); got != tc.stops {
			t.Errorf("stopsTheSweep(%v) = %v, want %v", err, got, tc.stops)
		}
	}
}
