# frozen_string_literal: true

require "test_helper"

# The Overview's Health section: Log processing's waits and Log reads' times
# from EventStore, and the Monitors, Issues and Events Console adds, which
# still show when EventStore can't answer.
class OverviewHealthTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper
  include MonitorTestHelper

  setup do
    @organization = create(:organization, slug: "acme")
    @member = create(:user)
    @organization.organization_users.create!(user: @member, role: :member)
    @web = create(:project, organization: @organization, name: "Web", slug: "web")
    @api = create(:project, organization: @organization, name: "Api", slug: "api")
    @theirs = create(:project, organization: create(:organization, slug: "theirs"), name: "Secret", slug: "secret")
    @overview_url = "#{EventStore.base_url}/overview"
    travel_to Time.utc(2026, 10, 2, 13, 0, 0)

    [ @web, @api, @theirs ].each { |project| stub_monitor_query_check(project) }
    @failing = create(:monitor, project: @web, name: "Checkout errors", last_run_at: 2.minutes.ago, last_error: "The Query has a mistake")
    @firing = create(:monitor, project: @api, name: "Slow requests")
    @firing.states.create!(group_key: "", status: "firing", since: Time.utc(2026, 10, 2, 12), last_value: 30)
    create(:monitor, project: @api, name: "Quiet").states.create!(group_key: "", status: "ok", since: 1.hour.ago)
    create(:monitor, project: @web, name: "Off", disabled_at: 1.day.ago, last_error: "Old failure")
    create(:monitor, project: @theirs, name: "Not ours", last_error: "Failed")

    @new_issue = create(:issue, project: @api, title: "NoMethodError in checkout", first_seen_at: Time.utc(2026, 10, 2, 9))
    create(:issue, project: @web, title: "Old", first_seen_at: Time.utc(2026, 9, 20))
    create(:issue, project: @theirs, title: "Theirs", first_seen_at: Time.utc(2026, 10, 2, 10))
  end

  # An answer of EventStore's /overview, Health and what the page needs.
  def answer
    {
      from: "2026-10-02", to: "2026-10-02", has_logs: true, ingest_counts_since: "2026-10-01T00:00:00Z",
      band: { received: { logs: 0, bytes: 0, bytes_on_wire: 0, requests: 0 }, unreadable_logs: 0, overloaded_requests: 0,
              stored: { logs: 0, bytes: 0, local_bytes: 0, bucket_bytes: 0, raw_bytes: 0 } },
      rates: { seconds: 46_800, logs: { average: 0, peak: 0, peak_at: nil }, bytes: { average: 0, peak: 0, peak_at: nil },
               requests: { average: 0, peak: 0, peak_at: nil }, last_five_minutes: nil },
      days: [],
      health: {
        processing: [ { project_id: @web.id, oldest_batch_at: "2026-10-02T12:58:00Z", waited_seconds: 120.0 } ],
        reads: {
          projects: [ { project_id: @api.id, reads: 12, median_ms: 40.5, p95_ms: 2500.0 } ],
          slowest: [ { project_id: @api.id, kind: "table", at: "2026-10-02T12:30:00Z", duration_ms: 3100.0 } ],
        },
        events: [ { project_id: @api.id, events: 7 }, { project_id: @web.id, events: 2 } ],
      },
    }
  end

  def health(path = overview_url(org_slug: "acme"))
    get path, headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    assert_response :success
    response.parsed_body.dig("props", "overview", "health")
  end

  test "a member sees each Project's waits and read times, the slowest reads, Monitors, new Issues and Events" do
    stub_request(:post, @overview_url).to_return(status: 200, body: answer.to_json)
    sign_in_as(@member)

    health = health()

    web, api = health["projects"]
    assert_equal [ "Web", "Api" ], [ web, api ].map { |p| p["project"]["name"] }, "in the Projects' order"
    assert_equal({ "oldest_batch_at" => "2026-10-02T12:58:00Z", "waited_seconds" => 120.0, "status" => "warning" }, web["processing"])
    assert_equal({ "oldest_batch_at" => nil, "waited_seconds" => 0, "status" => "ok" }, api["processing"])
    assert_nil web["reads"]
    assert_equal({ "reads" => 12, "median_ms" => 40.5, "p95_ms" => 2500.0, "status" => "warning" }, api["reads"])
    assert_equal [ 2, 7 ], [ web["events"], api["events"] ]

    slow = health["slowest_reads"].sole
    assert_equal [ "Api", "table", "2026-10-02T12:30:00Z", 3100.0, "warning" ],
      [ slow["project"]["name"], slow["kind"], slow["at"], slow["duration_ms"], slow["status"] ]

    failing = health["monitors"]["failing"].sole
    assert_equal [ "Checkout errors", "The Query has a mistake", "/acme/monitors/#{@failing.id}", "Web" ],
      failing.values_at("name", "last_error", "path").push(failing["project"]["name"])
    firing = health["monitors"]["firing"].sole
    assert_equal [ "Slow requests", 1, "2026-10-02T12:00:00Z", "/acme/monitors/#{@firing.id}" ],
      firing.values_at("name", "firing_count", "since", "path")

    assert_equal 1, health["issues"]["new"]
    issue = health["issues"]["newest"].sole
    assert_equal [ "NoMethodError in checkout", "/acme/projects/api/issues/#{@new_issue.number}", "2026-10-02T09:00:00Z" ],
      issue.values_at("title", "path", "first_seen_at")
    assert_equal({ "new" => 9 }, health["events"])
  end

  test "Monitors and Issues still show when EventStore can't be reached" do
    stub_request(:post, @overview_url).to_raise(Errno::ECONNREFUSED)
    sign_in_as(@member)

    health = health()

    assert_nil health["projects"]
    assert_nil health["slowest_reads"]
    assert_nil health["events"]
    assert_equal [ "Checkout errors" ], health["monitors"]["failing"].pluck("name")
    assert_equal [ "Slow requests" ], health["monitors"]["firing"].pluck("name")
    assert_equal 1, health["issues"]["new"]
  end

  test "Issues are new by the range's days" do
    stub_request(:post, @overview_url).to_return(status: 200, body: answer.to_json)
    sign_in_as(@member)

    assert_equal 2, health(overview_url(org_slug: "acme", range: "30d"))["issues"]["new"]
  end
end
