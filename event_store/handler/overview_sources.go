package handler

import (
	"cmp"
	"slices"

	"github.com/solidtrace/event_store/storage"
)

// maxSources is how many sources each of the Overview's rankings lists.
const maxSources = 10

// The Overview's Source counts: what Log processing kept and the Log
// transform dropped, in the band and each Project, and the biggest sources.
type (
	overviewKept struct {
		Logs  int64 `json:"logs"`
		Bytes int64 `json:"bytes"`
	}
	overviewSources struct {
		ByLogs  []overviewSource `json:"by_logs"`
		ByBytes []overviewSource `json:"by_bytes"`
	}
	overviewSource struct {
		ProjectID          uint32 `json:"project_id"`
		Service            string `json:"service"`
		File               string `json:"file"`
		Logs               int64  `json:"logs"`
		Bytes              int64  `json:"bytes"`
		DroppedByTransform int64  `json:"dropped_by_transform"`
	}
)

// assembleSources adds the Source counts to the answer: the band's and each
// Project's kept and dropped, and the sources ranked by Logs sent and by
// bytes kept.
func assembleSources(a *overviewAnswer, byProject map[uint32]int, counts []storage.SourceCount) {
	sources := make([]overviewSource, 0, len(counts))
	for _, c := range counts {
		a.Band.Kept.Logs += c.Logs
		a.Band.Kept.Bytes += c.Bytes
		a.Band.DroppedByTransform += c.DroppedByTransform
		p := &a.Projects[byProject[c.ProjectID]]
		p.Kept.Logs += c.Logs
		p.Kept.Bytes += c.Bytes
		p.DroppedByTransform += c.DroppedByTransform
		sources = append(sources, overviewSource(c))
	}
	// Of equals, the order is by Project, Service and file, so it holds
	// from one answer to the next.
	tie := func(a, b overviewSource) int {
		return cmp.Or(cmp.Compare(a.ProjectID, b.ProjectID), cmp.Compare(a.Service, b.Service), cmp.Compare(a.File, b.File))
	}
	top := func(by func(overviewSource) int64) []overviewSource {
		ranked := slices.Clone(sources)
		slices.SortFunc(ranked, func(a, b overviewSource) int { return cmp.Or(cmp.Compare(by(b), by(a)), tie(a, b)) })
		return ranked[:min(len(ranked), maxSources)]
	}
	a.Sources = overviewSources{
		// Sent: what reached Log processing, kept or dropped.
		ByLogs:  top(func(s overviewSource) int64 { return s.Logs + s.DroppedByTransform }),
		ByBytes: top(func(s overviewSource) int64 { return s.Bytes }),
	}
}
