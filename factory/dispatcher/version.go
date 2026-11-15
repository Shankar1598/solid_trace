package dispatcher

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/solidtrace/cli/dispatcherapi"
)

// SupportedMinorVersions is how many minor versions before its own the
// Dispatcher accepts from the solidtrace CLI. EventStore's agent ingest
// API has the same rule; the factory module cannot import EventStore
// (ADR 0019), so the rule is here again.
const SupportedMinorVersions = 3

// release is a release version, major.minor.patch.
type release struct {
	major, minor, patch int
}

var releaseVersion = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:[-+].*)?$`)

// parseRelease is nil for a version that is not a release, such as dev.
func parseRelease(text string) *release {
	match := releaseVersion.FindStringSubmatch(text)
	if match == nil {
		return nil
	}
	var parts [3]int
	for i := range parts {
		n, err := strconv.Atoi(match[i+1])
		if err != nil {
			return nil
		}
		parts[i] = n
	}
	return &release{major: parts[0], minor: parts[1], patch: parts[2]}
}

func (v release) String() string { return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch) }

func (v release) less(other release) bool {
	if v.major != other.major {
		return v.major < other.major
	}
	if v.minor != other.minor {
		return v.minor < other.minor
	}
	return v.patch < other.patch
}

// minimumCLI is the oldest solidtrace CLI that a Dispatcher of version
// accepts: SupportedMinorVersions minors before its own. While the
// Dispatcher has fewer minors than that in its major version, it accepts
// all of the major version before, because it does not know that major's
// last minor. Nil for a Dispatcher that is not a release: it accepts each
// CLI.
func minimumCLI(version string) *release {
	own := parseRelease(version)
	if own == nil {
		return nil
	}
	if own.minor < SupportedMinorVersions && own.major > 0 {
		return &release{major: own.major - 1}
	}
	return &release{major: own.major, minor: max(own.minor-SupportedMinorVersions, 0)}
}

// cliVersion is the version in a User-Agent that starts with
// solidtrace/<version>, as the solidtrace CLI sends it. Nil for another
// client (Console, a wrapper, curl) and for a CLI that is not a release.
func cliVersion(userAgent string) *release {
	product, _, _ := strings.Cut(strings.TrimSpace(userAgent), " ")
	name, version, ok := strings.Cut(product, "/")
	if !ok || !strings.EqualFold(name, "solidtrace") {
		return nil
	}
	return parseRelease(version)
}

// refuseOldCLIs answers 426 to a solidtrace CLI that is older than the
// minimum, before the token check: a new sign-in does not help that CLI.
func (d *Dispatcher) refuseOldCLIs(next http.Handler) http.Handler {
	minimum := minimumCLI(d.version)
	if minimum == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client := cliVersion(r.UserAgent())
		if client == nil || !client.less(*minimum) {
			next.ServeHTTP(w, r)
			return
		}
		writeJSON(w, http.StatusUpgradeRequired, dispatcherapi.Problem{
			Error: fmt.Sprintf("this solidtrace (%s) is too old for this Dispatcher (%s): install solidtrace %s or later",
				client, d.version, minimum),
			MinimumVersion: minimum.String(),
		})
	})
}
