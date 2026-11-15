package dispatcher

import (
	"context"
	"fmt"
)

// SetTicketStatus sets the status of a ticket of the tracker connection
// with name, as the write-back does, for the tests of a Tracker before
// the write-back exists.
func (d *Dispatcher) SetTicketStatus(ctx context.Context, name, ticket, status string) error {
	sources, err := d.store.webhookSources()
	if err != nil {
		return err
	}
	for _, source := range sources {
		t := d.trackers[source.Kind]
		if source.Name != name || t == nil {
			continue
		}
		saved, err := d.store.trackerSettings(source.ID)
		if err != nil {
			return err
		}
		return t.SetStatus(ctx, TrackerConnection{WebhookSource: source, Settings: trackerSettings(t, saved)}, ticket, status)
	}
	return fmt.Errorf("no tracker connection %s", name)
}
