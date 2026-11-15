package fakelinear

import "testing"

func TestComplexityFollowsLinearsRules(t *testing.T) {
	for _, c := range []struct {
		query string
		// want is in tenths of a point.
		want int
	}{
		{`query Viewer { viewer { id name } }`, 12},
		// A connection with no first multiplies its children by 50.
		{`query Q($id: String!) { issue(id: $id) { labels { nodes { name } } } }`, 10 + 10 + 50*11},
		{`query Q { issues(first: 2, filter: { state: { name: { eq: "x" } } }) { nodes { id state { name } } pageInfo { hasNextPage } } }`,
			10 + 2*((10+1+11)+(10+1))},
		{`mutation M($id: String!) { issueUpdate(id: $id, input: { stateId: "s" }) { success } }`, 11},
	} {
		if got, err := Complexity(c.query); err != nil || got != c.want {
			t.Errorf("Complexity(%s) = %d, %v, want %d", c.query, got, err, c.want)
		}
	}
}
