package dispatcher

import (
	"errors"
	"fmt"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/forge"
)

// projectForge gives the Forge of the Organization's connection with
// name, with the connection's token. The PR calls and the delete of a
// Run's work-in-progress ref go through it. Each error is fit for the
// reason on a Run.
func (d *Dispatcher) projectForge(organizationID, name string) (forge.Forge, error) {
	if name == "" {
		return nil, errors.New("the Project's Factory settings name no forge connection: " +
			"an Admin names one with 'solidtrace projects set'")
	}
	kind, token, err := d.store.connection(organizationID, name)
	if errors.Is(err, errNotFound) {
		return nil, fmt.Errorf("the Organization has no forge connection %s: an Admin makes it with "+
			"'solidtrace connections set'", name)
	}
	if err != nil {
		return nil, fmt.Errorf("read the forge connection %s: %w", name, err)
	}
	if kind != dispatcherapi.ForgeBitbucket {
		return nil, fmt.Errorf("the forge connection %s is of the kind %q, which this Dispatcher does not know", name, kind)
	}
	return &forge.Bitbucket{APIURL: d.config.BitbucketAPIURL, GitURL: d.config.BitbucketGitURL,
		Token: token, WIPRefs: d.config.WIPRefs}, nil
}

// repositoryForge is the Forge of the connection with name, or, for a
// Project that names no connection, Bitbucket with no token (a public
// repository).
func (d *Dispatcher) repositoryForge(organizationID, name string) (forge.Forge, error) {
	if name == "" {
		return &forge.Bitbucket{APIURL: d.config.BitbucketAPIURL, GitURL: d.config.BitbucketGitURL,
			WIPRefs: d.config.WIPRefs}, nil
	}
	return d.projectForge(organizationID, name)
}
