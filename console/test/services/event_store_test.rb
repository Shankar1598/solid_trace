# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

class EventStoreTest < ActiveSupport::TestCase
  setup do
    @base_url = "http://127.0.0.1:4100"
    @previous_url = ENV["EVENT_STORE_URL"]
    ENV["EVENT_STORE_URL"] = @base_url
  end

  teardown do
    ENV["EVENT_STORE_URL"] = @previous_url
  end

  test "get_event requests the event under its project" do
    stub_request(:get, "#{@base_url}/api/123/events/evt-123")
      .to_return(status: 200, body: { uuid: "evt-123" }.to_json, headers: { "Content-Type" => "application/json" })

    response = EventStore.get_event(project_id: 123, event_uuid: "evt-123")

    assert_equal "evt-123", response["uuid"]
  end

  test "query_events! answers no Events on not found" do
    stub_request(:get, "#{@base_url}/api/123/events")
      .to_return(status: 404, body: "")

    response = EventStore.query_events!(project_id: 123, params: {})

    assert_equal [], response
  end

  test "query_events! raises Unavailable with EventStore's reason when it can't read Events" do
    why = "Events can't be read now: ClickHouse doesn't answer: dial tcp 127.0.0.1:4200: connect: connection refused"
    stub_request(:get, "#{@base_url}/api/123/events").to_return(status: 503, body: { error: why }.to_json)

    error = assert_raises(EventStore::Unavailable) { EventStore.query_events!(project_id: 123) }

    assert_equal why, EventStore.error_text(error.body)
  end

  test "count_events returns count" do
    stub_request(:get, "#{@base_url}/api/123/events/count")
      .to_return(status: 200, body: { count: 7 }.to_json, headers: { "Content-Type" => "application/json" })

    count = EventStore.count_events(project_id: 123, params: {})

    assert_equal 7, count
  end

  test "query_logs runs a Query over a Project's time range" do
    stub_request(:post, "#{@base_url}/api/123/logs/query")
      .with(body: {
        query: "where level == \"error\"",
        from: "2026-10-03T09:00:00.000Z",
        to: "2026-10-03T10:00:00.000Z",
        cursor: "abc",
      }.to_json)
      .to_return(status: 200, body: { logs: [ { id: "log-1" } ], next_cursor: "def" }.to_json)

    page = EventStore.query_logs(
      project_id: 123,
      query: "where level == \"error\"",
      from: Time.utc(2026, 10, 3, 9),
      to: Time.utc(2026, 10, 3, 10),
      cursor: "abc"
    )

    assert_equal [ "log-1" ], page["logs"].map { |log| log["id"] }
    assert_equal "def", page["next_cursor"]
  end

  test "query_logs sends the values of the Dashboard filters a Query names as parameters" do
    run = stub_request(:post, "#{@base_url}/api/123/logs/query")
      .with(body: {
        query: "where service == $service | search $q",
        from: "2026-10-03T09:00:00.000Z",
        to: "2026-10-03T10:00:00.000Z",
        parameters: { service: [ "api", "web" ], q: [ "timeout" ] },
      }.to_json)
      .to_return(status: 200, body: { logs: [], next_cursor: nil }.to_json)

    EventStore.query_logs(
      project_id: 123,
      query: "where service == $service | search $q",
      from: Time.utc(2026, 10, 3, 9),
      to: Time.utc(2026, 10, 3, 10),
      parameters: { "service" => [ "api", "web" ], "q" => [ "timeout" ] }
    )

    assert_requested run
  end

  test "query_logs sends no parameters when there are none" do
    run = stub_request(:post, "#{@base_url}/api/123/logs/query")
      .with(body: { query: "where service == $service", from: "2026-10-03T09:00:00.000Z", to: "2026-10-03T10:00:00.000Z" }.to_json)
      .to_return(status: 200, body: { logs: [], next_cursor: nil }.to_json)

    [ nil, {} ].each do |parameters|
      EventStore.query_logs(project_id: 123, query: "where service == $service", from: Time.utc(2026, 10, 3, 9), to: Time.utc(2026, 10, 3, 10), parameters: parameters)
    end

    assert_requested run, times: 2
  end

  test "query_logs raises Rejected with EventStore's errors for a Query with mistakes" do
    errors = [ { message: "unknown stage", line: 1, column: 1, end_line: 1, end_column: 4 } ]
    stub_request(:post, "#{@base_url}/api/123/logs/query")
      .to_return(status: 400, body: { errors: errors }.to_json)

    error = assert_raises(EventStore::Rejected) do
      EventStore.query_logs(project_id: 123, query: "nope", from: Time.utc(2026, 10, 3, 9), to: Time.utc(2026, 10, 3, 10))
    end

    assert_equal "unknown stage", error.body["errors"].first["message"]
  end

  test "query_logs raises Unavailable when EventStore fails" do
    stub_request(:post, "#{@base_url}/api/123/logs/query").to_return(status: 500, body: { error: "Internal error" }.to_json)

    assert_raises(EventStore::Unavailable) do
      EventStore.query_logs(project_id: 123, query: "", from: Time.utc(2026, 10, 3, 9), to: Time.utc(2026, 10, 3, 10))
    end
  end

  test "query_logs asks EventStore to stop the run after timeout_seconds, and waits a little longer" do
    stub = stub_request(:post, "#{@base_url}/api/123/logs/query")
      .with(body: hash_including(query: "summarize count()", timeout_seconds: 30))
      .to_return(status: 200, body: { table: { columns: [], rows: [], truncated: false } }.to_json)
    read_timeouts = []
    execute = RestClient::Request.method(:execute)
    record = lambda do |args, &block|
      read_timeouts << args[:read_timeout]
      execute.call(args, &block)
    end

    RestClient::Request.stub(:execute, record) do
      EventStore.query_logs(project_id: 123, query: "summarize count()", from: Time.utc(2026, 10, 3, 9), to: Time.utc(2026, 10, 3, 10), timeout_seconds: 30)
    end

    assert_requested stub
    assert_equal [ 30 + EventStore::TIMEOUT_GRACE_SECONDS ], read_timeouts
  end

  test "query_logs raises TimedOut with EventStore's message when the run passes its timeout" do
    stub_request(:post, "#{@base_url}/api/123/logs/query")
      .to_return(status: 504, body: { error: "the run was stopped after 30 s, its timeout: try a shorter time range" }.to_json)

    error = assert_raises(EventStore::TimedOut) do
      EventStore.query_logs(project_id: 123, query: "", from: Time.utc(2026, 10, 3, 9), to: Time.utc(2026, 10, 3, 10), timeout_seconds: 30)
    end

    assert_equal "the run was stopped after 30 s, its timeout: try a shorter time range", error.body["error"]
    assert_kind_of EventStore::Unavailable, error
  end

  test "validate_logs_query returns errors and chips" do
    stub_request(:post, "#{@base_url}/api/123/logs/validate")
      .with(body: { query: "search \"boom\"" }.to_json)
      .to_return(status: 200, body: { errors: [], chips: { filters: [], search: "boom" } }.to_json)

    result = EventStore.validate_logs_query(project_id: 123, query: "search \"boom\"")

    assert_equal [], result["errors"]
    assert_equal "boom", result["chips"]["search"]
  end

  VRL_ERROR = {
    message: "call to undefined function", code: 105, start: 0, end: 12,
    text: "error[E105]: call to undefined function\n  ┌─ :1:1\n",
  }.freeze

  test "check_log_transform answers a program's errors and warnings" do
    stub_request(:post, "#{@base_url}/api/123/logs/transform/check")
      .with(body: { program: "http_request(\"x\")" }.to_json)
      .to_return(status: 200, body: { errors: [ VRL_ERROR ], warnings: [] }.to_json)

    result = EventStore.check_log_transform(project_id: 123, program: "http_request(\"x\")")

    assert_equal({ "errors" => [ VRL_ERROR.stringify_keys ], "warnings" => [] }, result)
  end

  test "preview_log_transform runs a draft on the recent arrivals, or on the Logs given" do
    answer = { results: [ { before: { message: "a" }, dropped: true } ], kept: 0, dropped: 1, failed: 0 }
    recent = stub_request(:post, "#{@base_url}/api/123/logs/transform/preview")
      .with(body: { program: "abort" }.to_json)
      .to_return(status: 200, body: answer.to_json)
    given = stub_request(:post, "#{@base_url}/api/123/logs/transform/preview")
      .with(body: { program: "abort", logs: [ { message: "a" } ] }.to_json)
      .to_return(status: 200, body: answer.to_json)

    assert_equal answer.deep_stringify_keys, EventStore.preview_log_transform(project_id: 123, program: "abort")
    assert_equal answer.deep_stringify_keys,
      EventStore.preview_log_transform(project_id: 123, program: "abort", logs: [ { message: "a" } ])
    assert_requested recent
    assert_requested given
  end

  test "preview_log_transform answers the check's errors for a draft that doesn't compile" do
    stub_request(:post, "#{@base_url}/api/123/logs/transform/preview")
      .to_return(status: 200, body: { errors: [ VRL_ERROR ], warnings: [] }.to_json)

    result = EventStore.preview_log_transform(project_id: 123, program: "http_request(\"x\")")

    assert_equal [ VRL_ERROR.stringify_keys ], result["errors"]
  end

  test "preview_log_transform raises Rejected for anything else EventStore refuses" do
    stub_request(:post, "#{@base_url}/api/123/logs/transform/preview")
      .to_return(status: 400, body: { error: "at most 200 logs" }.to_json)

    error = assert_raises(EventStore::Rejected) { EventStore.preview_log_transform(project_id: 123, program: ".", logs: [ {} ] * 201) }

    assert_equal "at most 200 logs", error.body["error"]
  end

  test "log_transform_stats answers the running counts of a Project's current version" do
    stats = { version_id: 7, processed: 10, dropped: 2, failed: 1, average_micros: 9.5 }
    stub_request(:get, "#{@base_url}/api/123/logs/transform/stats").to_return(status: 200, body: stats.to_json)

    assert_equal stats.stringify_keys, EventStore.log_transform_stats(project_id: 123)
  end

  test "get_log fetches one Log by id near its time" do
    stub_request(:get, "#{@base_url}/api/123/logs/0199a0b0-0000-7000-8000-000000000001")
      .with(query: { time: "2026-10-03T09:30:00.123Z" })
      .to_return(status: 200, body: { id: "0199a0b0-0000-7000-8000-000000000001" }.to_json)

    log = EventStore.get_log(project_id: 123, log_id: "0199a0b0-0000-7000-8000-000000000001", time: "2026-10-03T09:30:00.123Z")

    assert_equal "0199a0b0-0000-7000-8000-000000000001", log["id"]
  end

  test "get_log returns nil for a Log that isn't there" do
    stub_request(:get, "#{@base_url}/api/123/logs/0199a0b0-0000-7000-8000-000000000001")
      .to_return(status: 404, body: { error: "Log not found" }.to_json)

    assert_nil EventStore.get_log(project_id: 123, log_id: "0199a0b0-0000-7000-8000-000000000001")
  end

  test "get_log returns nil for an id that isn't a Log id, without asking EventStore" do
    fetch = stub_request(:get, %r{#{@base_url}/api/123/logs/.*})

    assert_nil EventStore.get_log(project_id: 123, log_id: "nope")
    assert_nil EventStore.get_log(project_id: 123, log_id: nil)
    assert_not_requested fetch
  end

  test "logs_fields lists a Project's Fields over a time range, up to a limit" do
    fields = {
      built_in: [ { name: "time", type: "datetime" }, { name: "level", type: "string" } ],
      attributes: [ { name: "status", type: "number", logs: 3 }, { name: "status", type: "string", logs: 1 } ],
      truncated: true,
    }
    stub_request(:get, "#{@base_url}/api/123/logs/fields")
      .with(query: { from: "2026-10-03T09:00:00.000Z", to: "2026-10-03T10:00:00.000Z", limit: "2" })
      .to_return(status: 200, body: fields.to_json)

    answer = EventStore.logs_fields(project_id: 123, from: Time.utc(2026, 10, 3, 9), to: Time.utc(2026, 10, 3, 10), limit: 2)

    assert_equal fields.deep_stringify_keys, answer
  end

  test "logs_fields leaves the limit to EventStore when none is given" do
    stub = stub_request(:get, "#{@base_url}/api/123/logs/fields")
      .with(query: { from: "2026-10-03T09:00:00.000Z", to: "2026-10-03T10:00:00.000Z" })
      .to_return(status: 200, body: { built_in: [], attributes: [], truncated: false }.to_json)

    EventStore.logs_fields(project_id: 123, from: Time.utc(2026, 10, 3, 9), to: Time.utc(2026, 10, 3, 10))

    assert_requested stub
  end

  test "logs_fields raises Rejected with EventStore's reason for a range or limit it refuses" do
    stub_request(:get, %r{\A#{@base_url}/api/123/logs/fields})
      .to_return(status: 400, body: { error: "limit must be a positive whole number" }.to_json)

    error = assert_raises(EventStore::Rejected) do
      EventStore.logs_fields(project_id: 123, from: Time.utc(2026, 10, 3, 9), to: Time.utc(2026, 10, 3, 10), limit: 0)
    end

    assert_equal "limit must be a positive whole number", error.body["error"]
  end

  test "logs_fields raises Unavailable when EventStore fails" do
    stub_request(:get, %r{\A#{@base_url}/api/123/logs/fields}).to_return(status: 500, body: { error: "Internal error" }.to_json)

    assert_raises(EventStore::Unavailable) do
      EventStore.logs_fields(project_id: 123, from: Time.utc(2026, 10, 3, 9), to: Time.utc(2026, 10, 3, 10))
    end
  end

  test "logs_histogram counts a Query's Logs per bucket over a Project's time range" do
    levels = { trace: 0, debug: 0, info: 2, warn: 0, error: 1, fatal: 0, none: 0 }
    stub_request(:post, "#{@base_url}/api/123/logs/histogram")
      .with(body: {
        query: "search \"boom\"",
        from: "2026-10-03T09:00:00.000Z",
        to: "2026-10-03T10:00:00.000Z",
        bucket_seconds: 60,
      }.to_json)
      .to_return(status: 200, body: {
        bucket_seconds: 60,
        buckets: [ { time: "2026-10-03T09:00:00Z", count: 3, levels: levels } ],
        count: 3,
        levels: levels,
      }.to_json)

    histogram = EventStore.logs_histogram(
      project_id: 123,
      query: "search \"boom\"",
      from: Time.utc(2026, 10, 3, 9),
      to: Time.utc(2026, 10, 3, 10),
      bucket_seconds: 60
    )

    assert_equal 3, histogram["count"]
    assert_equal 1, histogram["levels"]["error"]
    assert_equal [ 3 ], histogram["buckets"].map { |bucket| bucket["count"] }
  end

  test "logs_histogram leaves the bucket to EventStore when none is given" do
    stub_request(:post, "#{@base_url}/api/123/logs/histogram")
      .with(body: { query: "", from: "2026-10-03T09:00:00.000Z", to: "2026-10-03T10:00:00.000Z" }.to_json)
      .to_return(status: 200, body: { bucket_seconds: 60, buckets: [], count: 0, levels: {} }.to_json)

    histogram = EventStore.logs_histogram(project_id: 123, query: "", from: Time.utc(2026, 10, 3, 9), to: Time.utc(2026, 10, 3, 10))

    assert_equal 60, histogram["bucket_seconds"]
  end

  test "logs_histogram raises Rejected with EventStore's errors for a Query with mistakes" do
    stub_request(:post, "#{@base_url}/api/123/logs/histogram")
      .to_return(status: 400, body: { errors: [ { message: "unknown stage", line: 1, column: 1 } ] }.to_json)

    error = assert_raises(EventStore::Rejected) do
      EventStore.logs_histogram(project_id: 123, query: "nope", from: Time.utc(2026, 10, 3, 9), to: Time.utc(2026, 10, 3, 10))
    end

    assert_equal "unknown stage", error.body["errors"].first["message"]
  end

  test "readable_logs_sql prints a Query as SQL over a Project's time range" do
    InstanceSetting.current.update!(sql_queries_enabled: true)
    stub_request(:post, "#{@base_url}/api/123/logs/readable-sql")
      .with(body: { query: "take 5", from: "2026-10-03T09:00:00.000Z", to: "2026-10-03T10:00:00.000Z" }.to_json)
      .to_return(status: 200, body: { sql: "SELECT * FROM logs LIMIT 5" }.to_json)

    sql = EventStore.readable_logs_sql(project_id: 123, query: "take 5", from: Time.utc(2026, 10, 3, 9), to: Time.utc(2026, 10, 3, 10))

    assert_equal "SELECT * FROM logs LIMIT 5", sql["sql"]
  end

  test "run_logs_sql runs SQL over a Project's time range" do
    InstanceSetting.current.update!(sql_queries_enabled: true)
    stub_request(:post, "#{@base_url}/api/123/logs/sql")
      .with(body: { sql: "SELECT 1 AS one", from: "2026-10-03T09:00:00.000Z", to: "2026-10-03T10:00:00.000Z" }.to_json)
      .to_return(status: 200, body: { table: { columns: [ { name: "one", type: "number" } ], rows: [ [ 1 ] ], truncated: false } }.to_json)

    answer = EventStore.run_logs_sql(project_id: 123, sql: "SELECT 1 AS one", from: Time.utc(2026, 10, 3, 9), to: Time.utc(2026, 10, 3, 10))

    assert_equal [ [ 1 ] ], answer["table"]["rows"]
  end

  test "readable_logs_sql and run_logs_sql are refused while SQL queries are off" do
    InstanceSetting.current.update!(sql_queries_enabled: false)
    readable = stub_request(:post, "#{@base_url}/api/123/logs/readable-sql")
    sql = stub_request(:post, "#{@base_url}/api/123/logs/sql")
    range = { from: Time.utc(2026, 10, 3, 9), to: Time.utc(2026, 10, 3, 10) }

    assert_raises(EventStore::SQLQueriesOff) { EventStore.readable_logs_sql(project_id: 123, query: "", **range) }
    assert_raises(EventStore::SQLQueriesOff) { EventStore.run_logs_sql(project_id: 123, sql: "SELECT 1", **range) }

    assert_not_requested readable
    assert_not_requested sql
  end

  test "run_logs_sql is refused when EventStore has SQL queries off" do
    InstanceSetting.current.update!(sql_queries_enabled: true)
    stub_request(:post, "#{@base_url}/api/123/logs/sql")
      .to_return(status: 403, body: { error: "SQL queries are turned off on this instance" }.to_json)

    assert_raises(EventStore::SQLQueriesOff) do
      EventStore.run_logs_sql(project_id: 123, sql: "SELECT 1", from: Time.utc(2026, 10, 3, 9), to: Time.utc(2026, 10, 3, 10))
    end
  end

  test "a 403 from a Log call other than SQL is EventStore failing, not SQL queries off" do
    stub_request(:post, "#{@base_url}/api/123/logs/query").to_return(status: 403, body: { error: "Forbidden" }.to_json)

    assert_raises(EventStore::Unavailable) do
      EventStore.query_logs(project_id: 123, query: "", from: Time.utc(2026, 10, 3, 9), to: Time.utc(2026, 10, 3, 10))
    end
  end

  test "Log runs wait for EventStore however long they take, and other calls keep their timeouts" do
    InstanceSetting.current.update!(sql_queries_enabled: true)
    range = { from: Time.utc(2026, 10, 3, 9), to: Time.utc(2026, 10, 3, 10) }
    timeouts = {}
    record = lambda do |args, &_block|
      timeouts[args[:url].split("/").last] = {
        read: args.fetch(:read_timeout, :default),
        open: args.fetch(:open_timeout, :default),
      }
      Struct.new(:body).new("{}")
    end

    RestClient::Request.stub(:execute, record) do
      EventStore.query_logs(project_id: 123, query: "", **range)
      EventStore.logs_histogram(project_id: 123, query: "", **range)
      EventStore.run_logs_sql(project_id: 123, sql: "SELECT 1", **range)
      EventStore.validate_logs_query(project_id: 123, query: "")
      EventStore.logs_fields(project_id: 123, **range)
      EventStore.count_events!(project_id: 123)
    end

    unlimited = { read: nil, open: :default }
    default = { read: :default, open: :default }
    assert_equal(
      { "query" => unlimited, "histogram" => unlimited, "sql" => unlimited, "validate" => default, "fields" => default, "count" => default },
      timeouts
    )
  end

  test "all builds event objects" do
    issue = create(:issue)
    fingerprint = create(:issue_fingerprint, issue: issue)

    response = [
      {
        "uuid" => "evt-1",
        "project_id" => issue.project_id,
        "issue_fingerprint_id" => fingerprint.id,
        "timestamp" => Time.current.iso8601,
        "environment" => "production",
        "tags" => { "env" => "prod" }
      }
    ]

    EventStore.stub :query_events!, response do
      events = EventStore.new(issue).all

      assert_equal 1, events.length
      assert_instance_of Event, events.first
      assert_equal "evt-1", events.first.uuid
      assert_equal issue.project_id, events.first.project_id
    end
  end

  test "get_event_with_context returns nil when no fingerprints" do
    issue = build(:issue)

    result = EventStore.new(issue).get_event_with_context

    assert_nil result
  end
end
