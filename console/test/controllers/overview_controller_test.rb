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

  # An answer of EventStore's /overview.
  def answer(received_bytes: 4000, stored_bytes: 1000, since: "2026-10-02T09:00:00Z")
    {
      from: "2026-10-02", to: "2026-10-02", has_logs: true, ingest_counts_since: since,
      band: {
        received: { logs: 40, bytes: received_bytes, bytes_on_wire: 1500, requests: 4 },
        unreadable_logs: 2, overloaded_requests: 1,
        stored: { logs: 38, bytes: stored_bytes, local_bytes: stored_bytes, bucket_bytes: 0, raw_bytes: 3500 },
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
    assert_equal 38, band["stored"]["logs"]
    assert_equal 4.0, band["ratio"]
    assert_equal [ 2, 1 ], [ band["unreadable_logs"], band["overloaded_requests"] ]
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
