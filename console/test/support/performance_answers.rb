# frozen_string_literal: true

# EventStore's /transactions and /transaction answers for a Project, stood in
# at its URL, and what the Performance pages and their MCP tools make of
# them. The page tests and the tool tests check the same values, so the
# pages' props and the tools' answers agree.
module PerformanceAnswers
  # Production's Transactions as EventStore answers them: 3 sampled requests
  # of OrdersController#show standing for 28, and the Rack requests named by
  # their URL, folded into one name.
  TRANSACTIONS = {
    environments: [ "production", "staging" ],
    transactions: [
      {
        name: "OrdersController#show", sampled: 3, throughput: 28 / 3600.0, p50_us: 120_000, p95_us: 228_000, p99_us: 237_600,
        error_rate: 1 / 7.0, time_consumed_us: 2_640_000.0,
        previous: { sampled: 1, throughput: 4 / 3600.0, p50_us: 120_000, p95_us: 120_000, p99_us: 120_000, error_rate: 0, time_consumed_us: 480_000.0 },
      },
      {
        name: "<unparameterised>", sampled: 1, throughput: 4 / 3600.0, p50_us: 10_000, p95_us: 10_000, p99_us: 10_000,
        error_rate: 1, time_consumed_us: 40_000.0, previous: nil,
      },
    ],
  }.freeze

  SLOWEST_TRACE = "7e3f0e4a9b3d4e6f8a7b6c5d4e3f0002"

  # OrdersController#show's breakdown as EventStore answers it.
  TRANSACTION = {
    name: "OrdersController#show", sampled: 3, throughput: 28 / 3600.0, p50_us: 120_000, p95_us: 228_000, p99_us: 237_600,
    error_rate: 1 / 7.0, time_consumed_us: 2_640_000.0, bucket_seconds: 900,
    releases: [
      { release: "api@1.4.0", sampled: 2, p50_us: 180_000, p95_us: 234_000, points: [
        { time: "2026-10-02T11:00:00Z", sampled: 2, p50_us: 180_000, p95_us: 234_000 },
      ] },
      { release: "api@1.5.0", sampled: 1, p50_us: 60_000, p95_us: 60_000, points: [
        { time: "2026-10-02T11:30:00Z", sampled: 1, p50_us: 60_000, p95_us: 60_000 },
      ] },
    ],
    ops: [
      { op: "http.server", share: 0.3258, avg_self_time_us: 30_714 },
      { op: "db.sql.active_record", share: 0.1742, avg_self_time_us: 16_429 },
    ],
    groups: [
      { group: "00aa", op: "db.sql.active_record", description: %(SELECT "line_items".* FROM "line_items" WHERE "line_items"."id" = 1),
        normalised: %(SELECT "line_items".* FROM "line_items" WHERE "line_items"."id" = ?), share: 0.0909, avg_self_time_us: 8571 },
    ],
    repeats: [
      { group: "00aa", op: "db.sql.active_record", description: %(SELECT "line_items".* FROM "line_items" WHERE "line_items"."id" = 1),
        normalised: %(SELECT "line_items".* FROM "line_items" WHERE "line_items"."id" = ?), median: 2, max: 3, sampled: 3 },
    ],
    slowest: [
      { trace_id: SLOWEST_TRACE, start: "2026-10-02T11:20:00Z", duration_us: 240_000, status: "internal_error",
        release: "api@1.4.0", environment: "production" },
    ],
  }.freeze

  def stub_transactions(project, from:, to:, environment: "")
    stub_request(:post, "#{EventStore.base_url}/api/#{project.id}/transactions")
      .with(body: { from: from, to: to, environment: environment }.to_json)
      .to_return(status: 200, body: TRANSACTIONS.merge(from: from, to: to).to_json)
  end

  def stub_transaction(project, name, from:, to:, environment: "")
    stub_request(:post, "#{EventStore.base_url}/api/#{project.id}/transaction")
      .with(body: { name: name, from: from, to: to, environment: environment }.to_json)
      .to_return(status: 200, body: TRANSACTION.merge(from: from, to: to).to_json)
  end

  # Performance as the page and list_transactions answer it.
  def assert_performance(performance, span:, environment:)
    assert_equal({ "available" => true }, performance["event_store"])
    assert_equal span, performance.dig("range", "span")
    environment ? assert_equal(environment, performance["environment"]) : assert_nil(performance["environment"])
    assert_equal [ "production", "staging" ], performance["environments"]
    assert_equal({ "id" => @api.id, "name" => "Api", "slug" => "api" }, performance["project"])

    orders, unparameterised = performance["transactions"]
    assert_equal [ "OrdersController#show", "OrdersController#show" ], orders.values_at("name", "label")
    # Throughput a minute, estimated from 28 requests in an hour's sample.
    assert_in_delta 28 / 60.0, orders["throughput_per_minute"], 1e-9
    assert_equal [ 3, 120_000, 228_000, 237_600, 2_640_000.0 ], orders.values_at("sampled", "p50_us", "p95_us", "p99_us", "time_consumed_us")
    assert_in_delta 1 / 7.0, orders["error_rate"], 1e-9
    assert_in_delta 4 / 60.0, orders.dig("previous", "throughput_per_minute"), 1e-9
    # Each number's change: a share of the previous number, and the error
    # rate's in points.
    change = orders["change"]
    assert_in_delta 6.0, change["throughput_per_minute"], 1e-9
    assert_in_delta 0.0, change["p50_us"], 1e-9
    assert_in_delta 0.9, change["p95_us"], 1e-9
    assert_in_delta 0.98, change["p99_us"], 1e-9
    assert_in_delta 4.5, change["time_consumed_us"], 1e-9
    assert_in_delta 1 / 7.0, change["error_rate"], 1e-9

    assert_equal [ "<unparameterised>", "Unparameterised" ], unparameterised.values_at("name", "label")
    assert_nil unparameterised["previous"]
    assert_nil unparameterised["change"]
  end

  # OrdersController#show as its page and get_transaction answer it.
  def assert_transaction(transaction)
    assert_equal({ "available" => true }, transaction["event_store"])
    assert_equal [ "OrdersController#show", "OrdersController#show" ], transaction.values_at("name", "label")
    assert_equal [ 3, 120_000, 228_000 ], transaction["summary"].values_at("sampled", "p50_us", "p95_us")
    assert_in_delta 28 / 60.0, transaction.dig("summary", "throughput_per_minute"), 1e-9
    assert_equal 900, transaction["bucket_seconds"]
    assert_equal [ [ "api@1.4.0", 180_000, 234_000 ], [ "api@1.5.0", 60_000, 60_000 ] ],
      transaction["releases"].map { |r| r.values_at("release", "p50_us", "p95_us") }
    assert_equal [ "2026-10-02T11:00:00Z" ], transaction["releases"].first["points"].map { |p| p["time"] }
    assert_equal [ [ "http.server", 0.3258 ], [ "db.sql.active_record", 0.1742 ] ], transaction["ops"].map { |o| o.values_at("op", "share") }
    assert_equal [ %(SELECT "line_items".* FROM "line_items" WHERE "line_items"."id" = ?) ], transaction["groups"].map { |g| g["normalised"] }
    assert_equal [ [ 2, 3, 3 ] ], transaction["repeats"].map { |r| r.values_at("median", "max", "sampled") }
    assert_equal [ [ SLOWEST_TRACE, 240_000, "internal_error" ] ],
      transaction["slowest"].map { |s| s.values_at("trace_id", "duration_us", "status") }
  end
end
