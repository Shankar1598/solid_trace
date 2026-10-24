# frozen_string_literal: true

require "test_helper"

# get_overview: an Organization's Overview, the sections its page shows.
class McpOverviewToolsTest < ActiveSupport::TestCase
  include McpTestHelper

  setup do
    @user = create(:user)
    @acme = create(:organization, slug: "acme")
    create(:organization_user, organization: @acme, user: @user, role: :member)
    @web = create(:project, organization: @acme, slug: "web")
    create(:project, organization: create(:organization, slug: "theirs"), slug: "secret")
    @overview_url = "#{EventStore.base_url}/overview"
    travel_to Time.utc(2026, 10, 2, 13, 0, 0)
  end

  def answer
    {
      has_logs: true, ingest_counts_since: "2026-09-01T00:00:00Z",
      band: {
        received: { logs: 40, bytes: 4000, bytes_on_wire: 1500, requests: 4 },
        unreadable_logs: 2, overloaded_requests: 0,
        stored: { logs: 38, bytes: 500, local_bytes: 500, bucket_bytes: 0, raw_bytes: 3500 },
      },
      rates: {
        seconds: 600_000,
        logs: { average: 0.0001, peak: 12, peak_at: "2026-10-01T10:00:05Z" },
        bytes: { average: 0.01, peak: 900, peak_at: "2026-10-01T10:00:05Z" },
        requests: { average: 0.00001, peak: 2, peak_at: "2026-10-01T10:00:00Z" },
        last_five_minutes: { logs_per_second: 0.5 },
      },
    }
  end

  test "get_overview answers the band and rates of the Organization's Projects for a range, linking to the page" do
    asked = stub_request(:post, @overview_url)
      .with(body: { project_ids: [ @web.id ], from: "2026-09-26", to: "2026-10-02", include_storage: false }.to_json)
      .to_return(status: 200, body: answer.to_json)

    got = call_tool(:get_overview, user: @user, range: "7d")

    assert_not got.error?
    assert_requested asked
    assert_equal "7d", got.json.dig("range", "name")
    assert_equal({ "logs" => 40, "bytes" => 4000, "bytes_on_wire" => 1500, "requests" => 4 }, got.json.dig("band", "received"))
    assert_equal 8.0, got.json.dig("band", "ratio")
    assert_equal({ "average" => 0.0001, "peak" => 12, "peak_at" => "2026-10-01T10:00:05Z" }, got.json.dig("rates", "logs"))
    assert_equal "http://console.test/acme/overview?range=7d", got.link
  end

  test "get_overview is Today by default, and says when EventStore can't answer" do
    stub_request(:post, @overview_url).with(body: hash_including(from: "2026-10-02")).to_raise(Errno::ECONNREFUSED)

    got = call_tool(:get_overview, user: @user)

    assert_not got.error?
    assert_equal false, got.json.dig("event_store", "available")
    assert_nil got.json["band"]
    assert_equal "http://console.test/acme/overview", got.link
  end

  test "get_overview refuses a range it doesn't know and an Organization that isn't yours" do
    assert call_tool(:get_overview, user: @user, organization: "theirs").error?
    assert call_tool(:get_overview, user: @user, range: "1y").error?
  end

  test "the server instructions name get_overview" do
    assert_includes McpServer::INSTRUCTIONS, "get_overview"
  end
end
