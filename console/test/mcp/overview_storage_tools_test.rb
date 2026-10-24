# frozen_string_literal: true

require "test_helper"

# get_overview's Storage: the whole install's, for Instance admins only, as
# the Overview page shows it.
class McpOverviewStorageToolsTest < ActiveSupport::TestCase
  include McpTestHelper

  setup do
    @member = create(:user)
    @admin = create(:user, instance_admin: true)
    @acme = create(:organization, slug: "acme")
    create(:organization_user, organization: @acme, user: @member, role: :member)
    create(:organization_user, organization: @acme, user: @admin, role: :member)
    create(:project, organization: @acme, slug: "web")
    @overview_url = "#{EventStore.base_url}/overview"
    travel_to Time.utc(2026, 10, 2, 13, 0, 0)
  end

  def answer(storage: nil)
    {
      has_logs: true, ingest_counts_since: "2026-09-01T00:00:00Z",
      band: { received: { logs: 0, bytes: 0, bytes_on_wire: 0, requests: 0 }, unreadable_logs: 0, overloaded_requests: 0,
              stored: { logs: 0, bytes: 0, local_bytes: 0, bucket_bytes: 0, raw_bytes: 0 } },
      rates: { seconds: 46_800, logs: { average: 0, peak: 0, peak_at: nil }, bytes: { average: 0, peak: 0, peak_at: nil },
               requests: { average: 0, peak: 0, peak_at: nil }, last_five_minutes: nil },
      storage: storage,
    }.compact
  end

  test "get_overview answers an Instance admin the whole install's Storage, with a link" do
    stub_request(:post, @overview_url).with(body: hash_including(include_storage: true))
      .to_return(status: 200, body: answer(storage: {
        disk_limit_bytes: 1000, local_bytes: 250, disk_bytes: 2000, free_bytes: 900, bucket: true, bucket_bytes: 4000,
        growth_bytes_per_day: 10, days_until_limit: nil, old_files: nil,
      }).to_json)

    got = call_tool(:get_overview, user: @admin)

    assert_not got.error?
    storage = got.json["storage"]
    assert_equal true, storage["whole_install"]
    assert_equal [ 250, 1000, 0.25, 4000 ], storage.values_at("local_bytes", "disk_limit_bytes", "limit_share", "bucket_bytes")
    assert_equal "http://console.test/acme/overview", got.link
  end

  test "get_overview answers no Storage to a member who isn't an Instance admin" do
    asked = stub_request(:post, @overview_url).with(body: hash_including(include_storage: false))
      .to_return(status: 200, body: answer.to_json)

    got = call_tool(:get_overview, user: @member)

    assert_not got.error?
    assert_requested asked
    assert_not got.json.key?("storage")
  end
end
