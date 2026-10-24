# frozen_string_literal: true

require "test_helper"

# The Overview page: an Organization's band and rates over Today, 7 or 30
# UTC days, from EventStore's /overview, for any member.
class OverviewControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  setup do
    @organization = create(:organization, slug: "acme")
    @member = create(:user)
    @organization.organization_users.create!(user: @member, role: :member)
    @web = create(:project, organization: @organization, name: "Web", slug: "web")
    @api = create(:project, organization: @organization, name: "Api", slug: "api")
    @theirs = create(:project, organization: create(:organization, slug: "theirs"), name: "Secret", slug: "secret")
    @overview_url = "#{EventStore.base_url}/overview"
    travel_to Time.utc(2026, 10, 2, 13, 0, 0)
  end

  # An answer of EventStore's /overview. Every stored day has Source counts
  # unless stored_counted_bytes says less.
  def answer(received_bytes: 4000, kept_bytes: 3000, stored_bytes: 1000, stored_counted_bytes: stored_bytes,
             since: "2026-10-02T09:00:00Z", source_counts_since: "2026-09-01")
    {
      from: "2026-10-02", to: "2026-10-02", has_logs: true, ingest_counts_since: since, source_counts_since: source_counts_since,
      band: {
        received: { logs: 40, bytes: received_bytes, bytes_on_wire: 1500, requests: 4 },
        unreadable_logs: 2, overloaded_requests: 1,
        kept: { logs: 35, bytes: kept_bytes }, dropped_by_transform: 3,
        stored: { logs: 38, bytes: stored_bytes, local_bytes: stored_bytes, bucket_bytes: 0, raw_bytes: 3500 },
        stored_counted_bytes: stored_counted_bytes,
      },
      rates: {
        seconds: 46_800,
        logs: { average: 0.001, peak: 12, peak_at: "2026-10-02T10:00:05Z" },
        bytes: { average: 0.08, peak: 900, peak_at: "2026-10-02T10:00:05Z" },
        requests: { average: 0.0001, peak: 2, peak_at: "2026-10-02T10:00:00Z" },
        last_five_minutes: { logs_per_second: 0.5 },
      },
      days: [ { day: "2026-10-02", stored: { logs: 38, bytes: stored_bytes } } ],
    }
  end

  def overview_props(path)
    get path, headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    assert_response :success
    assert_equal "Overview/Show", response.parsed_body["component"]
    response.parsed_body.dig("props", "overview")
  end

  test "a member sees their Organization's band and rates for Today by default" do
    asked = stub_request(:post, @overview_url)
      .with(body: { project_ids: [ @web.id, @api.id ], from: "2026-10-02", to: "2026-10-02", include_storage: false }.to_json)
      .to_return(status: 200, body: answer.to_json)
    sign_in_as(@member)

    overview = overview_props(overview_url(org_slug: "acme"))

    assert_requested asked
    assert_equal({ "name" => "today", "from" => "2026-10-02", "to" => "2026-10-02", "days" => 1, "time_zone" => "UTC" }, overview["range"])
    assert_equal({ "available" => true }, overview["event_store"])
    assert_equal({ "projects" => false, "logs" => false }, overview["empty"])
    band = overview["band"]
    assert_equal({ "logs" => 40, "bytes" => 4000, "bytes_on_wire" => 1500, "requests" => 4 }, band["received"])
    assert_equal({ "logs" => 35, "bytes" => 3000 }, band["kept"])
    assert_equal 38, band["stored"]["logs"]
    assert_equal 3.0, band["ratio"], "kept bytes over stored"
    assert_equal [ 2, 3, 1 ], band.values_at("unreadable_logs", "dropped_by_transform", "overloaded_requests")
    assert_equal({ "average" => 0.001, "peak" => 12, "peak_at" => "2026-10-02T10:00:05Z" }, overview["rates"]["logs"])
    assert_equal({ "logs_per_second" => 0.5 }, overview["rates"]["last_five_minutes"])
    assert_equal({ "since" => "2026-10-02T09:00:00Z", "starts_after_range" => true }, overview["ingest_counts"])
  end

  test "the range comes from the URL, and anything else is Today" do
    seven = stub_request(:post, @overview_url).with(body: hash_including(from: "2026-09-26", to: "2026-10-02"))
      .to_return(status: 200, body: answer(since: "2026-09-01T00:00:00Z").to_json)
    thirty = stub_request(:post, @overview_url).with(body: hash_including(from: "2026-09-03", to: "2026-10-02"))
      .to_return(status: 200, body: answer.to_json)
    today = stub_request(:post, @overview_url).with(body: hash_including(from: "2026-10-02", to: "2026-10-02"))
      .to_return(status: 200, body: answer.to_json)
    sign_in_as(@member)

    week = overview_props(overview_url(org_slug: "acme", range: "7d"))
    assert_equal [ "7d", 7 ], week["range"].values_at("name", "days")
    assert_nil week["rates"]["last_five_minutes"], "only Today has the last five minutes"
    assert_equal false, week["ingest_counts"]["starts_after_range"]
    assert_equal "30d", overview_props(overview_url(org_slug: "acme", range: "30d"))["range"]["name"]
    assert_equal "today", overview_props(overview_url(org_slug: "acme", range: "1y"))["range"]["name"]
    assert_requested seven
    assert_requested thirty
    assert_requested today
  end

  test "another Organization's Overview is not found" do
    sign_in_as(@member)

    get overview_url(org_slug: "theirs")

    assert_response :not_found
  end

  test "the page renders what Console holds when EventStore can't be reached" do
    stub_request(:post, @overview_url).to_raise(Errno::ECONNREFUSED)
    sign_in_as(@member)

    overview = overview_props(overview_url(org_slug: "acme"))

    assert_equal false, overview["event_store"]["available"]
    assert_match(/can't be reached/, overview["event_store"]["error"])
    assert_nil overview["band"]
    assert_nil overview["rates"]
    assert_equal "today", overview["range"]["name"]
  end

  test "the page says what EventStore said when it fails" do
    stub_request(:post, @overview_url).to_return(status: 500, body: { error: "Internal error" }.to_json)
    sign_in_as(@member)

    assert_match(/unavailable/, overview_props(overview_url(org_slug: "acme"))["event_store"]["error"])
  end

  test "an Organization without Projects is empty and asks EventStore nothing" do
    lonely = create(:organization, slug: "lonely")
    lonely.organization_users.create!(user: @member, role: :member)
    asked = stub_request(:post, @overview_url)
    sign_in_as(@member)

    overview = overview_props(overview_url(org_slug: "lonely"))

    assert_not_requested asked
    assert_equal({ "projects" => true, "logs" => true }, overview["empty"])
  end

  test "an Organization whose Projects have no Logs yet is empty of Logs" do
    stub_request(:post, @overview_url).to_return(status: 200, body: answer.merge(has_logs: false).to_json)
    sign_in_as(@member)

    assert_equal({ "projects" => false, "logs" => true }, overview_props(overview_url(org_slug: "acme"))["empty"])
  end

  # An answer's day: the Logs each Project received, by id, and the bytes.
  def day(day, logs, received_bytes: 0, stored_bytes: 0, peak: 0)
    {
      day: day, stored: { logs: 0, bytes: stored_bytes },
      received: { logs: logs.values.sum, bytes: received_bytes, bytes_on_wire: 0, requests: 1 },
      logs_per_second: { average: logs.values.sum / 86_400.0, peak: peak, peak_at: peak.positive? ? "#{day}T10:00:00Z" : nil },
      projects: logs.map { |id, n| { project_id: id, received_logs: n } },
    }
  end

  # An answer's Project.
  def project(id, logs: 0, received_bytes: 0, kept_bytes: received_bytes, stored_bytes: 0, stored_counted_bytes: stored_bytes,
              all_logs: 0, oldest_day: nil, last_log_at: nil)
    {
      project_id: id,
      received: { logs: logs, bytes: received_bytes, bytes_on_wire: received_bytes, requests: 1 },
      stored: { logs: logs, bytes: stored_bytes, local_bytes: stored_bytes, bucket_bytes: 0, raw_bytes: 0 },
      logs_per_second: { average: logs / 86_400.0, peak: logs, peak_at: logs.positive? ? "2026-10-02T10:00:00Z" : nil },
      all_days: { logs: all_logs, bytes: stored_bytes * 3, oldest_day: oldest_day },
      last_log_at: last_log_at,
      kept: { logs: logs, bytes: kept_bytes }, dropped_by_transform: 1, stored_counted_bytes: stored_counted_bytes,
    }
  end

  test "the ratio compares kept bytes with the stored bytes of only the days with Source counts, with a notice when they begin in the range" do
    projects = [ project(@web.id, logs: 30, received_bytes: 3000, kept_bytes: 2400, stored_bytes: 600, stored_counted_bytes: 300) ]
    stub_request(:post, @overview_url).to_return(status: 200, body: answer(
      kept_bytes: 3000, stored_bytes: 1000, stored_counted_bytes: 500, source_counts_since: "2026-09-30"
    ).merge(projects: projects).to_json)
    sign_in_as(@member)

    overview = overview_props(overview_url(org_slug: "acme", range: "7d"))

    assert_equal 6.0, overview["band"]["ratio"], "3,000 kept over the 500 stored of the counted days"
    assert_equal 8.0, overview["projects"][0]["ratio"], "2,400 kept over 300"
    assert_equal({ "since" => "2026-09-30", "starts_after_range" => true }, overview["source_counts"])
  end

  test "with no day's Source counts there's no ratio" do
    projects = [ project(@web.id, logs: 30, received_bytes: 3000, kept_bytes: 0, stored_bytes: 600, stored_counted_bytes: 0) ]
    stub_request(:post, @overview_url).to_return(status: 200, body: answer(
      kept_bytes: 0, stored_bytes: 1000, stored_counted_bytes: 0, source_counts_since: nil
    ).merge(projects: projects).to_json)
    sign_in_as(@member)

    overview = overview_props(overview_url(org_slug: "acme"))

    assert_nil overview["band"]["ratio"]
    assert_nil overview["projects"][0]["ratio"]
    assert_equal({ "since" => nil, "starts_after_range" => true }, overview["source_counts"])
  end

  test "each Project wears a Chart colour in creation order, and the eighth on fold into Other" do
    # @web and @api came first; seven more make nine.
    more = (1..7).map { |n| create(:project, organization: @organization, name: "More #{n}", created_at: Time.utc(2026, 10, 1) + n.minutes) }
    @web.update!(created_at: Time.utc(2026, 9, 1))
    @api.update!(created_at: Time.utc(2026, 9, 2))
    ids = [ @web, @api, *more ].map(&:id)
    days = [
      day("2026-10-01", { ids[1] => 5, ids[7] => 2, ids[8] => 1 }, received_bytes: 900, stored_bytes: 90),
      day("2026-10-02", { ids[0] => 3, ids[8] => 4 }, received_bytes: 700, stored_bytes: 70, peak: 4),
    ]
    stub_request(:post, @overview_url).with(body: hash_including(project_ids: ids))
      .to_return(status: 200, body: answer.merge(days: days, projects: ids.map { |id| project(id) }).to_json)
    sign_in_as(@member)

    daily = overview_props(overview_url(org_slug: "acme", range: "7d"))["daily"]

    # Web takes the first colour though it has no Logs the first day.
    series = daily["series"]
    assert_equal [ "Web", "Api", *(1..5).map { |n| "More #{n}" }, "Other" ], series.map { |s| s["name"] }
    assert_equal [ *1..8 ], series.map { |s| s["color"] }
    assert_equal ids[7..], series.last["project_ids"]
    keys = series.map { |s| s["key"] }
    assert_equal({ keys[1] => 5, "other" => 3 }, daily["days"][0]["logs"].select { |_, n| n.positive? })
    assert_equal({ keys[0] => 3, "other" => 4 }, daily["days"][1]["logs"].select { |_, n| n.positive? })
    assert_equal [ "2026-10-01", 900, 90 ], daily["days"][0].values_at("day", "received_bytes", "stored_bytes")
    assert_equal({ "average" => 7 / 86_400.0, "peak" => 4, "peak_at" => "2026-10-02T10:00:00Z" }, daily["days"][1]["logs_per_second"])

    rows = overview_props(overview_url(org_slug: "acme", range: "7d"))["projects"]
    assert_equal [ *1..7, 8, 8 ], rows.map { |r| r["color"] }
    assert_equal [ "Web", "Api" ], rows.first(2).map { |r| r["name"] }
  end

  test "the Projects table answers each Project's numbers, Retention and last Log" do
    @web.update!(created_at: Time.utc(2026, 9, 1), log_retention_days: 7)
    @api.update!(created_at: Time.utc(2026, 9, 2))
    InstanceSetting.current.update!(default_log_retention_days: 45)
    projects = [
      project(@web.id, logs: 30, received_bytes: 3000, kept_bytes: 2400, stored_bytes: 600, all_logs: 90, oldest_day: "2026-09-26", last_log_at: "2026-10-02T12:30:00Z"),
      project(@api.id, logs: 10, received_bytes: 1000, stored_bytes: 0, all_logs: 10, oldest_day: "2026-10-01", last_log_at: "2026-10-02T11:59:59Z"),
    ]
    stub_request(:post, @overview_url).to_return(status: 200, body: answer.merge(projects: projects).to_json)
    sign_in_as(@member)

    web, api = overview_props(overview_url(org_slug: "acme"))["projects"]

    assert_equal({ "id" => @web.id, "name" => "Web", "slug" => "web", "color" => 1 }, web.slice("id", "name", "slug", "color"))
    assert_equal [ 30, 3000, 2400, 600, 4.0, 0.75 ], web.values_at("logs", "received_bytes", "kept_bytes", "stored_bytes", "ratio", "share")
    assert_equal 1, web["dropped_by_transform"]
    assert_equal({ "average" => 30 / 86_400.0, "peak" => 30, "peak_at" => "2026-10-02T10:00:00Z" }, web["logs_per_second"])
    assert_equal [ 7, false ], web.values_at("retention_days", "retention_is_default")
    assert_equal [ "2026-09-26", { "logs" => 90, "bytes" => 1800 } ], web.values_at("oldest_day", "all_days")
    assert_equal [ "2026-10-02T12:30:00Z", false, true ], web.values_at("last_log_at", "stale", "has_logs")
    # Api's Retention is the instance's, its last Log over an hour ago.
    assert_equal [ 45, true ], api.values_at("retention_days", "retention_is_default")
    assert_equal [ nil, 0.25, true ], api.values_at("ratio", "share", "stale")
  end

  test "a Project with no Logs yet says so" do
    projects = [ project(@web.id), project(@api.id, logs: 1, all_logs: 1, oldest_day: "2026-10-02", last_log_at: "2026-10-02T12:59:00Z") ]
    stub_request(:post, @overview_url).to_return(status: 200, body: answer.merge(projects: projects).to_json)
    sign_in_as(@member)

    rows = overview_props(overview_url(org_slug: "acme"))["projects"]

    web = rows.find { |r| r["slug"] == "web" }
    assert_equal [ false, nil, nil, nil ], web.values_at("has_logs", "last_log_at", "stale", "oldest_day")
    assert rows.find { |r| r["slug"] == "api" }["has_logs"]
  end

  test "the biggest sources answer each source's Project, Logs, bytes, drops and share" do
    source = ->(id, service, file, logs, bytes, dropped) do
      { project_id: id, service: service, file: file, logs: logs, bytes: bytes, dropped_by_transform: dropped }
    end
    sources = {
      by_logs: [ source.(@api.id, "worker", "", 20, 500, 5), source.(@web.id, "web", "a.log", 10, 2500, 0) ],
      by_bytes: [ source.(@web.id, "web", "a.log", 10, 2500, 0), source.(@api.id, "worker", "", 20, 500, 5) ],
    }
    # 35 Logs kept and 3 dropped make 38 sent; 3000 bytes kept.
    stub_request(:post, @overview_url).to_return(status: 200, body: answer.merge(sources: sources).to_json)
    sign_in_as(@member)

    got = overview_props(overview_url(org_slug: "acme"))["sources"]

    worker, web = got["by_logs"]
    assert_equal({ "id" => @api.id, "name" => "Api", "slug" => "api", "color" => 2 }, worker["project"])
    assert_equal [ "worker", "", 20, 500, 5, 25, 0.6579 ], worker.values_at("service", "file", "logs", "bytes", "dropped_by_transform", "sent", "share")
    assert_equal [ "web", "a.log", 0.2632 ], web.values_at("service", "file", "share")
    assert_equal [ [ "a.log", 0.8333 ], [ "", 0.1667 ] ], got["by_bytes"].map { |s| s.values_at("file", "share") }
  end

  test "the daily Charts and Projects are absent when EventStore can't be reached" do
    stub_request(:post, @overview_url).to_raise(Errno::ECONNREFUSED)
    sign_in_as(@member)

    overview = overview_props(overview_url(org_slug: "acme"))

    assert_nil overview["daily"]
    assert_nil overview["projects"]
  end

  test "an Instance admin's Overview asks for Storage" do
    admin = create(:user, instance_admin: true)
    @organization.organization_users.create!(user: admin, role: :member)
    asked = stub_request(:post, @overview_url).with(body: hash_including(include_storage: true))
      .to_return(status: 200, body: answer.to_json)
    sign_in_as(admin)

    overview_props(overview_url(org_slug: "acme"))

    assert_requested asked
  end
end
