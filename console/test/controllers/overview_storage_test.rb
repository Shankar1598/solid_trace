# frozen_string_literal: true

require "test_helper"

# The Overview's Storage section: the whole install's disk, bucket and old
# Parquet files, from EventStore's /overview, for Instance admins only.
class OverviewStorageTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  setup do
    @organization = create(:organization, slug: "acme")
    @member = create(:user)
    @organization.organization_users.create!(user: @member, role: :member)
    @admin = create(:user, instance_admin: true)
    @organization.organization_users.create!(user: @admin, role: :member)
    create(:project, organization: @organization, name: "Web", slug: "web")
    @overview_url = "#{EventStore.base_url}/overview"
    travel_to Time.utc(2026, 10, 2, 13, 0, 0)
  end

  # An answer of EventStore's /overview, with Storage when asked for it.
  def answer(storage: nil)
    {
      from: "2026-10-02", to: "2026-10-02", has_logs: true, ingest_counts_since: "2026-10-01T00:00:00Z",
      band: { received: { logs: 0, bytes: 0, bytes_on_wire: 0, requests: 0 }, unreadable_logs: 0, overloaded_requests: 0,
              stored: { logs: 0, bytes: 0, local_bytes: 0, bucket_bytes: 0, raw_bytes: 0 } },
      rates: { seconds: 46_800, logs: { average: 0, peak: 0, peak_at: nil }, bytes: { average: 0, peak: 0, peak_at: nil },
               requests: { average: 0, peak: 0, peak_at: nil }, last_five_minutes: nil },
      days: [],
      storage: storage,
    }.compact
  end

  def storage_answer
    {
      disk_limit_bytes: 50_000_000_000, local_bytes: 20_000_000_000, disk_bytes: 100_000_000_000,
      free_bytes: 30_000_000_000, bucket: false, bucket_bytes: 0,
      growth_bytes_per_day: 1_500_000_000.4, projected_local_bytes: 80_000_000_000, stays_under_limit: false, days_until_limit: 20.7,
      old_files: { files: 1200, local_bytes: 3_000_000_000, newest_day: "2026-09-14", passes_retention_on: "2026-10-15" },
    }
  end

  def overview_props
    get overview_url(org_slug: "acme"), headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    assert_response :success
    response.parsed_body.dig("props", "overview")
  end

  test "an Instance admin sees the whole install's disk against the limit, the bucket and the old Parquet files" do
    stub_request(:post, @overview_url).with(body: hash_including(include_storage: true))
      .to_return(status: 200, body: answer(storage: storage_answer).to_json)
    sign_in_as(@admin)

    storage = overview_props["storage"]

    assert_equal true, storage["whole_install"]
    assert_equal [ 20_000_000_000, 50_000_000_000, 0.4 ], storage.values_at("local_bytes", "disk_limit_bytes", "limit_share")
    assert_equal [ 100_000_000_000, 30_000_000_000 ], storage.values_at("disk_bytes", "free_bytes")
    assert_equal [ false, 0 ], storage.values_at("bucket", "bucket_bytes")
    assert_equal [ 1_500_000_000, 80_000_000_000, false, 20 ],
                 storage.values_at("growth_bytes_per_day", "projected_local_bytes", "stays_under_limit", "days_until_limit")
    assert_equal({ "files" => 1200, "local_bytes" => 3_000_000_000, "newest_day" => "2026-09-14", "passes_retention_on" => "2026-10-15" },
                 storage["old_files"])
  end

  test "a disk that stays under the limit at steady state has its projected size and no day count" do
    stub_request(:post, @overview_url).to_return(status: 200, body: answer(storage: storage_answer.merge(
      projected_local_bytes: 40_000_000_000, stays_under_limit: true, days_until_limit: nil
    )).to_json)
    sign_in_as(@admin)

    storage = overview_props["storage"]

    assert_equal [ 40_000_000_000, true, nil ], storage.values_at("projected_local_bytes", "stays_under_limit", "days_until_limit")
  end

  test "with a bucket and no old files, there are no days until the limit and no old files" do
    stub_request(:post, @overview_url)
      .to_return(status: 200, body: answer(storage: storage_answer.merge(bucket: true, bucket_bytes: 9_000, days_until_limit: nil, old_files: nil)).to_json)
    sign_in_as(@admin)

    storage = overview_props["storage"]

    assert_equal [ true, 9_000 ], storage.values_at("bucket", "bucket_bytes")
    assert_nil storage["days_until_limit"]
    assert_nil storage["old_files"]
  end

  test "a member who isn't an Instance admin has no Storage section, and EventStore isn't asked for it" do
    asked = stub_request(:post, @overview_url).with(body: hash_including(include_storage: false))
      .to_return(status: 200, body: answer.to_json)
    sign_in_as(@member)

    overview = overview_props

    assert_requested asked
    assert_not overview.key?("storage")
  end

  test "an Instance admin's Storage is empty when EventStore can't be reached, and the page still renders" do
    stub_request(:post, @overview_url).to_raise(Errno::ECONNREFUSED)
    sign_in_as(@admin)

    overview = overview_props

    assert overview.key?("storage")
    assert_nil overview["storage"]
    assert_equal false, overview.dig("event_store", "available")
  end
end
