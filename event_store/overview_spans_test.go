package main_test

import (
	"encoding/json"
	"testing"
	"time"
)

// overviewSpans is the Spans of the Overview's answer, of all its Projects
// or of one: received by second of arrival, stored by the Spans' own days.
type overviewSpans struct {
	Received      int64 `json:"received"`
	ReceivedBytes int64 `json:"received_bytes"`
	Stored        int64 `json:"stored"`
	StoredBytes   int64 `json:"stored_bytes"`
	LocalBytes    int64 `json:"local_bytes"`
	BucketBytes   int64 `json:"bucket_bytes"`
}

// overviewSpansOf asks for the Overview of projects over the UTC days from
// to to, and answers its Spans in total and each Project's, in order.
func overviewSpansOf(t *testing.T, env *TestEnv, from, to time.Time, projects ...uint32) (overviewSpans, []overviewSpans) {
	t.Helper()
	status, raw := postQueryApp(t, env, "/overview", overviewRequest{
		ProjectIDs: projects, From: from.Format(time.DateOnly), To: to.Format(time.DateOnly),
	})
	if status != 200 {
		t.Fatalf("Expected the Overview, got %d: %s", status, raw)
	}
	var answer struct {
		Spans    overviewSpans `json:"spans"`
		Projects []struct {
			Spans overviewSpans `json:"spans"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("Decode the Overview %s: %v", raw, err)
	}
	each := make([]overviewSpans, len(answer.Projects))
	for i, p := range answer.Projects {
		each[i] = p.Spans
	}
	return answer.Spans, each
}

func TestTheOverviewAnswersSpansReceivedAndStoredPerProjectAndInTotal(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProjectKey(t, env.SQLitePath, 124, "go_public_key")

	// Two Rails requests of 7 Spans each to Project 123, and the service
	// they called to 124. The Spans start on 2 October, as they arrive.
	postEnvelopeWithKey(t, env, "test_public_key", transactionFixture(t, "sentry-ruby-6.3-rails"))
	postEnvelopeWithKey(t, env, "test_public_key", transactionFixture(t, "sentry-ruby-5.28-rails"))
	postEnvelopeWithKey(t, env, "go_public_key", transactionFixture(t, "sentry-go-0.47"))
	env.Clock.Advance(time.Second)
	env.FlushIngestCounts(t)

	// Received counts them on arrival, before Span processing stores them.
	total, each := overviewSpansOf(t, env, testNow, testNow, 123, 124, 456)
	goSpans := int64(len(goRows))
	if total.Received != 14+goSpans || each[0].Received != 14 || each[1].Received != goSpans || each[2].Received != 0 {
		t.Errorf("Expected 14 Spans received by 123 and %d by 124, got %d in all and %+v", goSpans, total.Received, each)
	}
	if each[0].ReceivedBytes <= 0 || each[1].ReceivedBytes <= 0 || total.ReceivedBytes != each[0].ReceivedBytes+each[1].ReceivedBytes {
		t.Errorf("Expected each Project's Transactions' bytes received, summed in all, got %+v and %+v", total, each)
	}
	if total.Stored != 0 {
		t.Errorf("Expected no Spans stored before Span processing, got %+v", total)
	}

	env.ProcessSpans(t)
	total, each = overviewSpansOf(t, env, testNow, testNow, 123, 124, 456)
	if total.Stored != 14+goSpans || each[0].Stored != 14 || each[1].Stored != goSpans || each[2].Stored != 0 {
		t.Errorf("Expected 14 Spans stored of 123 and %d of 124, got %d in all and %+v", goSpans, total.Stored, each)
	}
	if each[0].StoredBytes <= 0 || total.StoredBytes != total.LocalBytes+total.BucketBytes ||
		total.StoredBytes != each[0].StoredBytes+each[1].StoredBytes {
		t.Errorf("Expected the stored bytes on local disk, summed in all, got %+v and %+v", total, each)
	}

	// Spans aren't Logs: Projects that sent only Spans have none.
	got := overview(t, env, testNow, testNow, 123, 124)
	if got.Band.Received.Logs != 0 || got.Band.Received.Requests != 0 || got.Band.Stored.Logs != 0 {
		t.Errorf("Expected no Logs received or stored, got %+v", got.Band)
	}
	if got.HasLogs {
		t.Error("Expected Projects that sent only Spans to have no Logs")
	}
	// Nor are they another day's.
	dayBefore := testNow.AddDate(0, 0, -1)
	if total, _ := overviewSpansOf(t, env, dayBefore, dayBefore, 123, 124); total != (overviewSpans{}) {
		t.Errorf("Expected no Spans the day before, got %+v", total)
	}
}
