package storage

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The order of the Agent sessions list (spec, The agent ingest API: the
// read endpoints for Console). It has no database: the seam is the totals
// of the Agent sessions, as agentSessionTotals sums them.

var orderAt = time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)

func orderSession(id string, startMinute, lastMinute int, tokens uint64, cost *float64) AgentSessionTotals {
	return AgentSessionTotals{
		SessionID: uuid.MustParse(id),
		First:     orderAt.Add(time.Duration(startMinute) * time.Minute),
		Last:      orderAt.Add(time.Duration(lastMinute) * time.Minute),
		// The tokens of each kind add up to the tokens of the sort.
		Input: tokens / 4, Output: tokens / 4, CacheRead: tokens / 4, CacheWrite: tokens - 3*(tokens/4),
		CostUSD: cost,
	}
}

func costOf(usd float64) *float64 { return &usd }

const (
	orderA = "00000000-0000-4000-8000-00000000000a"
	orderB = "00000000-0000-4000-8000-00000000000b"
	orderC = "00000000-0000-4000-8000-00000000000c"
	orderD = "00000000-0000-4000-8000-00000000000d"
	orderE = "00000000-0000-4000-8000-00000000000e"
)

// orderSessions are five Agent sessions: B and C tie on each number, D
// and E have no cost, and E ties with A on its last activity.
func orderSessions() []AgentSessionTotals {
	return []AgentSessionTotals{
		orderSession(orderE, 5, 50, 30, nil),
		orderSession(orderC, 20, 40, 2000, costOf(0.5)),
		orderSession(orderA, 10, 50, 100, costOf(0.1)),
		orderSession(orderD, 0, 10, 9000, nil),
		orderSession(orderB, 20, 40, 2000, costOf(0.5)),
	}
}

func orderedIDs(sessions []AgentSessionTotals) []string {
	ids := []string{}
	for _, s := range sessions {
		ids = append(ids, s.SessionID.String())
	}
	return ids
}

func TestTheAgentSessionsSortByEachColumnInBothDirectionsWithTiesByID(t *testing.T) {
	for _, c := range []struct {
		name  string
		order AgentSessionOrder
		want  []string
	}{
		{"the default: the latest activity first", AgentSessionOrder{}, []string{orderA, orderE, orderB, orderC, orderD}},
		{"the last activity, latest first", AgentSessionOrder{Sort: AgentSortLast}, []string{orderA, orderE, orderB, orderC, orderD}},
		{"the last activity, earliest first", AgentSessionOrder{Sort: AgentSortLast, Ascending: true}, []string{orderD, orderB, orderC, orderA, orderE}},
		{"the start, latest first", AgentSessionOrder{Sort: AgentSortStart}, []string{orderB, orderC, orderA, orderE, orderD}},
		{"the start, earliest first", AgentSessionOrder{Sort: AgentSortStart, Ascending: true}, []string{orderD, orderE, orderA, orderB, orderC}},
		{"the tokens, most first", AgentSessionOrder{Sort: AgentSortTokens}, []string{orderD, orderB, orderC, orderA, orderE}},
		{"the tokens, fewest first", AgentSessionOrder{Sort: AgentSortTokens, Ascending: true}, []string{orderE, orderA, orderB, orderC, orderD}},
		// An Agent session with no cost comes after those with a cost, in
		// both directions.
		{"the cost, highest first", AgentSessionOrder{Sort: AgentSortCost}, []string{orderB, orderC, orderA, orderD, orderE}},
		{"the cost, lowest first", AgentSessionOrder{Sort: AgentSortCost, Ascending: true}, []string{orderA, orderB, orderC, orderD, orderE}},
	} {
		t.Run(c.name, func(t *testing.T) {
			sessions := orderSessions()
			c.order.sortSessions(sessions)
			if got := orderedIDs(sessions); !slices.Equal(got, c.want) {
				t.Errorf("Expected %v, got %v", c.want, got)
			}
		})
	}
}

func TestTheTokensOfAnAgentSessionAreTheTokensOfEachKindAdded(t *testing.T) {
	s := AgentSessionTotals{Input: 1, Output: 20, CacheRead: 300, CacheWrite: 4000}
	if got := s.Tokens(); got != 4321 {
		t.Errorf("Expected 4321 tokens, got %d", got)
	}
}

func TestOnlyTheFourSortsAreAgentSessionSorts(t *testing.T) {
	for _, sort := range []string{AgentSortLast, AgentSortStart, AgentSortTokens, AgentSortCost} {
		if !IsAgentSort(sort) {
			t.Errorf("Expected %q to be a sort", sort)
		}
	}
	for _, sort := range []string{"", "title", "Tokens", "last "} {
		if IsAgentSort(sort) {
			t.Errorf("Expected %q not to be a sort", sort)
		}
	}
}
