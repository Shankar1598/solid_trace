# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

class ChartRunsTest < ActiveSupport::TestCase
  test "leaves spare Puma threads free of chart runs, and runs one on a Console of few threads" do
    assert_equal 7, ChartRuns.limit(10)
    assert_equal 1, ChartRuns.limit(4)
    assert_equal 1, ChartRuns.limit(1)
  end

  test "refuses a run past the limit, and frees a slot as each run ends" do
    holding_every_chart_run do
      assert_raises(ChartRuns::Busy) { ChartRuns.hold { flunk "ran past the limit" } }
    end

    assert_equal :ran, ChartRuns.hold { :ran }
  end

  test "frees the slot of a run that fails" do
    ChartRuns.limit.times do
      assert_raises(EventStore::Unavailable) { ChartRuns.hold { raise EventStore::Unavailable, "down" } }
    end

    holding_every_chart_run { pass }
  end

  test "counts the runs of every thread" do
    started = Queue.new
    finish = Queue.new
    threads = Array.new(ChartRuns.limit) do
      Thread.new { ChartRuns.hold { started << true; finish.pop } }
    end
    ChartRuns.limit.times { started.pop }

    assert_raises(ChartRuns::Busy) { ChartRuns.hold { flunk "ran past the limit" } }
  ensure
    threads&.each { finish << true }
    threads&.each(&:join)
  end

  test "a chart run answers EventStore's table for the Query over the range, with the chart timeout" do
    project = create(:project)
    from, to = Time.utc(2026, 10, 3, 9), Time.utc(2026, 10, 3, 10)
    table = { "columns" => [ { "name" => "count_", "type" => "number" } ], "rows" => [ [ 12 ] ], "truncated" => false }
    stub_request(:post, "#{EventStore.base_url}/api/#{project.id}/logs/query")
      .with(body: { query: "summarize count()", from: "2026-10-03T09:00:00.000Z", to: "2026-10-03T10:00:00.000Z",
                    timeout_seconds: EventStoreProxy::RUN_TIMEOUT_SECONDS }.to_json)
      .to_return(status: 200, body: { table: table, elapsed_ms: 41 }.to_json)

    answer = ChartRuns.run_chart(project, "summarize count()", from: from, to: to)

    assert_equal table, answer["table"]
    assert_equal 41, answer["elapsed_ms"]
  end

  test "a chart run passes the Dashboard filters' values on to EventStore" do
    project = create(:project)
    table = { "columns" => [ { "name" => "count_", "type" => "number" } ], "rows" => [ [ 3 ] ], "truncated" => false }
    run = stub_request(:post, "#{EventStore.base_url}/api/#{project.id}/logs/query")
      .with(body: { query: "where service == $service | summarize count()", from: "2026-10-03T09:00:00.000Z", to: "2026-10-03T10:00:00.000Z",
                    timeout_seconds: EventStoreProxy::RUN_TIMEOUT_SECONDS, parameters: { service: [ "api" ] } }.to_json)
      .to_return(status: 200, body: { table: table }.to_json)

    answer = ChartRuns.run_chart(project, "where service == $service | summarize count()",
                                 from: Time.utc(2026, 10, 3, 9), to: Time.utc(2026, 10, 3, 10), parameters: { "service" => [ "api" ] })

    assert_requested run
    assert_equal table, answer["table"]
  end

  test "a chart run whose Query answers Logs is refused as no table" do
    project = create(:project)
    stub_request(:post, "#{EventStore.base_url}/api/#{project.id}/logs/query").to_return(status: 200, body: { logs: [], next_cursor: nil }.to_json)

    error = assert_raises(ChartRuns::NotATable) { ChartRuns.run_chart(project, "where level == 'error'", from: 1.hour.ago, to: Time.current) }
    assert_match(/answers Logs, not a table.*summarize/, error.message)
  end

  test "a chart run is one of the runs ChartRuns allows" do
    project = create(:project)
    run = stub_request(:post, "#{EventStore.base_url}/api/#{project.id}/logs/query")

    holding_every_chart_run do
      assert_raises(ChartRuns::Busy) { ChartRuns.run_chart(project, "summarize count()", from: 1.hour.ago, to: Time.current) }
    end
    assert_not_requested run
  end

  private
    def holding_every_chart_run(left = ChartRuns.limit, &block)
      left.zero? ? yield : ChartRuns.hold { holding_every_chart_run(left - 1, &block) }
    end
end
