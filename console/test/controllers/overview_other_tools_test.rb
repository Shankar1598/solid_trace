# frozen_string_literal: true

require "test_helper"

# The Overview's Compared with other tools: what the range's volume, scaled
# to 30 days from the seconds it has had, would cost at Datadog's, Better Stack's and Axiom's list prices
# at each Project's Retention, and GCP's internet egress for the bytes on the
# wire. The expected dollars are worked by hand from the vendors' pricing
# pages (checked 2026-10-24).
class OverviewOtherToolsTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  setup do
    @organization = create(:organization, slug: "acme")
    @member = create(:user)
    @organization.organization_users.create!(user: @member, role: :member)
    @web = create(:project, organization: @organization, name: "Web", slug: "web", log_retention_days: 10)
    @api = create(:project, organization: @organization, name: "Api", slug: "api")
    InstanceSetting.current.update!(default_log_retention_days: 45)
    @overview_url = "#{EventStore.base_url}/overview"
    travel_to Time.utc(2026, 10, 2, 13, 0, 0)
  end

  # An answer of EventStore's /overview over a range that has had seconds
  # so far, 7 whole days by default: Web received 7 GB in 7 million Logs and
  # Api 700 GB in 70 million, 700 GiB on the wire.
  def answer(seconds: 7 * 86_400)
    received = ->(logs, bytes) { { logs: logs, bytes: bytes, bytes_on_wire: 0, requests: 1 } }
    {
      from: "2026-09-26", to: "2026-10-02", has_logs: true, ingest_counts_since: "2026-09-01T00:00:00Z",
      band: { received: { logs: 77_000_000, bytes: 707_000_000_000, bytes_on_wire: 700 * 2**30, requests: 2 },
              unreadable_logs: 0, overloaded_requests: 0,
              stored: { logs: 0, bytes: 0, local_bytes: 0, bucket_bytes: 0, raw_bytes: 0 } },
      rates: { seconds: seconds, logs: { average: 0, peak: 0, peak_at: nil }, bytes: { average: 0, peak: 0, peak_at: nil },
               requests: { average: 0, peak: 0, peak_at: nil }, last_five_minutes: nil },
      days: [],
      projects: [
        { project_id: @web.id, received: received.(7_000_000, 7_000_000_000) },
        { project_id: @api.id, received: received.(70_000_000, 700_000_000_000) },
      ],
    }
  end

  def other_tools(range)
    get overview_url(org_slug: "acme", range: range), headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    assert_response :success
    response.parsed_body.dig("props", "overview", "other_tools")
  end

  test "a member sees each tool's estimate and the egress for the range scaled to 30 days, at each Project's Retention" do
    stub_request(:post, @overview_url).to_return(status: 200, body: answer.to_json)
    sign_in_as(@member)

    got = other_tools("7d")

    assert_equal "2026-10-24", got["checked_on"]
    assert_equal({ "days" => 7, "seconds" => 604_800, "scaled" => true, "logs" => 330_000_000, "bytes" => 3_030_000_000_000,
                   "bytes_on_wire" => 3000 * 2**30 }, got["volume"])

    by_key = got["tools"].index_by { |t| t["key"] }
    assert_equal %w[datadog better_stack axiom], by_key.keys
    # Web: 30 GB × $0.10 + 30M Logs × $2.55 (15 days, the shortest ≥ 10) = $79.50.
    # Api: 3,000 GB × $0.10 + 300M × $3.75 (30 days, the longest listed) = $1,425.
    assert_equal 1504.5, by_key["datadog"]["monthly_usd"]
    assert by_key["datadog"]["beyond_list"], "Api's 45 days are past Datadog's listed retention"
    # Web: 30 GB × $0.10 + 30 GB × 10/30 × $0.05 = $3.50.
    # Api: 3,000 GB × $0.10 + 3,000 GB × 45/30 × $0.05 = $525.
    assert_equal 528.5, by_key["better_stack"]["monthly_usd"]
    # $25 + (3,030 − 1,000 included) GB × $0.12 + ((10 + 4,500) GB-months ÷ 10 − 100 free) × $0.03.
    assert_equal 279.13, by_key["axiom"]["monthly_usd"]
    assert(got["tools"].all? { |t| t["url"].start_with?("https://") && t["plan"].present? })

    # 1,024 GiB × $0.12 + 1,976 GiB × $0.11.
    assert_equal 340.24, got["egress"]["monthly_usd"]
    assert_equal "https://cloud.google.com/vpc/network-pricing", got["egress"]["url"]
  end

  test "a 30-day range isn't scaled, and Projects within Datadog's listed Retention aren't beyond it" do
    @api.update!(log_retention_days: 3)
    stub_request(:post, @overview_url).to_return(status: 200, body: answer(seconds: 30 * 86_400).to_json)
    sign_in_as(@member)

    got = other_tools("30d")

    assert_equal({ "days" => 30, "seconds" => 2_592_000, "scaled" => false, "logs" => 77_000_000, "bytes" => 707_000_000_000,
                   "bytes_on_wire" => 700 * 2**30 }, got["volume"])
    datadog = got["tools"].find { |t| t["key"] == "datadog" }
    # Web: 7 GB × $0.10 + 7M × $2.55; Api: 700 GB × $0.10 + 70M × $1.59 (3 days).
    assert_equal 199.85, datadog["monthly_usd"]
    assert_not datadog["beyond_list"]
  end

  test "Today at 06:00 is scaled by 30 days over the 6 hours it has had, not over a whole day" do
    travel_to Time.utc(2026, 10, 2, 6, 0, 0)
    stub_request(:post, @overview_url).to_return(status: 200, body: answer(seconds: 6 * 3600).to_json)
    sign_in_as(@member)

    got = other_tools("today")

    # 30 days over 6 hours is 120 times what arrived.
    assert_equal({ "days" => 1, "seconds" => 21_600, "scaled" => true, "logs" => 9_240_000_000,
                   "bytes" => 84_840_000_000_000, "bytes_on_wire" => 84_000 * 2**30 }, got["volume"])
    # Web: 840 GB × $0.10 + 840 GB × 10/30 × $0.05 = $98.
    # Api: 84,000 GB × $0.10 + 84,000 GB × 45/30 × $0.05 = $14,700.
    assert_equal 14_798.0, got["tools"].find { |t| t["key"] == "better_stack" }["monthly_usd"]
  end

  test "the section is null when EventStore can't answer" do
    stub_request(:post, @overview_url).to_raise(Errno::ECONNREFUSED)
    sign_in_as(@member)

    assert_nil other_tools("7d")
  end
end
