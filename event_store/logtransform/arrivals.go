package logtransform

import (
	"bytes"
	"container/list"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/ingest/normalise"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/storage"
)

// The caps on a Project's recent arrivals.
const (
	arrivalsPerService = 100
	arrivalsBytes      = 4 << 20
)

// arrivals are each Project's recent arrivals: its newest Logs as they
// arrived, before any program, up to arrivalsPerService of each Service and
// about arrivalsBytes in all. The oldest go first. They are kept in memory
// only, for previews: once stored, a Log as it arrived is gone.
type arrivals struct {
	mu       sync.Mutex
	projects map[uint32]*projectArrivals
}

// projectArrivals are one Project's recent arrivals.
type projectArrivals struct {
	order    *list.List                 // of arrival, oldest first
	services map[string][]*list.Element // each Service's, oldest first
	bytes    int
	// last is the newest Log batch whose Logs were added. Log processing
	// reads a Project's batches in order, and reads them again when they
	// fail to store.
	last uuid.UUID
}

// arrival is a Log as it arrived, before any program, and when SolidTrace
// received it.
type arrival struct {
	log        models.Log
	receivedAt time.Time
	bytes      int
}

// freshFrom returns the index of the first of a Project's batches whose Logs
// haven't been added.
func (a *arrivals) freshFrom(projectID uint32, batches []storage.LogBatch) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := a.projects[projectID]
	for i, batch := range batches {
		if p == nil || bytes.Compare(batch.ID[:], p.last[:]) > 0 {
			return i
		}
	}
	return len(batches)
}

// add adds the Logs of a Project's batches, which come after those added
// before.
func (a *arrivals) add(projectID uint32, batches []storage.LogBatch) {
	if len(batches) == 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.projects == nil {
		a.projects = make(map[uint32]*projectArrivals)
	}
	p := a.projects[projectID]
	if p == nil {
		p = &projectArrivals{order: list.New(), services: make(map[string][]*list.Element)}
		a.projects[projectID] = p
	}
	for _, batch := range batches {
		at := receivedAt(batch.ID)
		for _, log := range batch.Logs {
			p.push(arrival{log: log, receivedAt: at, bytes: normalise.Size(log)})
		}
		p.last = batch.ID
	}
}

// newest returns a Project's recent arrivals, newest first.
func (a *arrivals) newest(projectID uint32) []arrival {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := a.projects[projectID]
	if p == nil {
		return nil
	}
	out := make([]arrival, 0, p.order.Len())
	for e := p.order.Back(); e != nil; e = e.Prev() {
		out = append(out, e.Value.(arrival))
	}
	return out
}

// push adds a Log, dropping the oldest of its Service, or of all, past the
// caps.
func (p *projectArrivals) push(a arrival) {
	service := a.log.Service
	p.services[service] = append(p.services[service], p.order.PushBack(a))
	p.bytes += a.bytes
	if len(p.services[service]) > arrivalsPerService {
		p.dropOldest(service)
	}
	for p.bytes > arrivalsBytes {
		p.dropOldest(p.order.Front().Value.(arrival).log.Service)
	}
}

// dropOldest drops a Service's oldest Log.
func (p *projectArrivals) dropOldest(service string) {
	queue := p.services[service]
	p.bytes -= p.order.Remove(queue[0]).(arrival).bytes
	if len(queue) == 1 {
		delete(p.services, service)
	} else {
		p.services[service] = queue[1:]
	}
}
