# frozen_string_literal: true

require "test_helper"
require "active_job/test_helper"
require "webmock/minitest"

# The Notification outbox delivers to a 'Factory' Integration as to Slack,
# in the generic signed webhook's format: one request for each Issue, and
# one for each change of a Monitor's group, to the Dispatcher's
# /webhooks/generic/<sender id>. Observes only the requests that the
# Dispatcher gets and the Notification rows' outcomes.
class FactoryNotifierTest < ActiveSupport::TestCase
  include ActiveJob::TestHelper
  include ActiveSupport::Testing::TimeHelpers
  include MonitorTestHelper

  DISPATCHER = "http://10.100.0.3:8080"
  SECRET = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
  WEBHOOK_URL = "#{DISPATCHER}/webhooks/generic/snd_1"
  NOW = Time.utc(2026, 11, 15, 12, 0, 0)

  setup do
    travel_to NOW
    InstanceSetting.current.update!(dispatcher_url: DISPATCHER)
    @organization = create(:organization, slug: "acme")
    @project = create(:project, organization: @organization, name: "Shop")
    @factory = @organization.integrations.create!(provider: "factory", sender_id: "snd_1", sender_secret: SECRET,
      settings: { "notify_on_event_threshold" => "0" })
    @webhook = stub_request(:post, WEBHOOK_URL).to_return(accepted)
  end

  teardown do
    travel_back
  end

  test "each Issue of a Notification is its own signed request, with the Issue as the dedupe_key" do
    first = create(:issue, project: @project, title: "NoMethodError in checkout", culprit: "CartsController#show")
    second = create(:issue, project: @project, title: "Timeout in search")
    IntegrationNotification.issue_received_event(first, newly_created: true)
    IntegrationNotification.issue_received_event(second, newly_created: true)

    IntegrationNotification.deliver_pending(@factory.id)

    deliveries = received
    assert_equal 2, deliveries.size
    rows = Notification.order(:id).to_a
    assert rows.all?(&:status_sent?)
    assert_equal rows.map { |row| row.id.to_s }, deliveries.map { |d| d[:delivery] }
    assert_equal [ "solid-trace-issue-#{first.id}", "solid-trace-issue-#{second.id}" ], deliveries.map { |d| d[:body]["dedupe_key"] }

    body = deliveries.first[:body]
    assert_equal %w[attributes dedupe_key link project prompt title], body.keys.sort
    assert_equal "shop", body["project"]
    assert_equal "NoMethodError in checkout", body["title"]
    assert_equal "http://example.com/acme/projects/shop/issues/#{first.number}", body["link"]
    assert_includes body["prompt"], "NoMethodError in checkout"
    assert_includes body["prompt"], "CartsController#show"
    assert_includes body["prompt"], body["link"]
    assert_equal({ "solidtrace.issue_number" => first.number.to_s, "solidtrace.notification" => "issue_created" },
      body["attributes"])
  end

  test "the signature is HMAC-SHA256 of the timestamp, a '.' and the raw body, with the sender's secret" do
    IntegrationNotification.issue_received_event(create(:issue, project: @project), newly_created: true)

    IntegrationNotification.deliver_pending(@factory.id)

    assert_requested(:post, WEBHOOK_URL, times: 1) do |request|
      assert_equal "application/json", request.headers["Content-Type"]
      assert_equal NOW.to_i.to_s, request.headers["Solidtrace-Timestamp"]
      want = "sha256=" + OpenSSL::HMAC.hexdigest("SHA256", SECRET, "#{NOW.to_i}.#{request.body}")
      assert_equal want, request.headers["Solidtrace-Signature"]
      other = "sha256=" + OpenSSL::HMAC.hexdigest("SHA256", "another secret", "#{NOW.to_i}.#{request.body}")
      assert_not_equal other, request.headers["Solidtrace-Signature"]
    end
  end

  test "a Monitor's change is one request with the Monitor and its group as the dedupe_key" do
    stub_monitor_query_check(@project)
    monitor = create(:monitor, project: @project, name: "Checkout errors", integrations: [ @factory ])
    change(monitor, IntegrationNotification::MONITOR_FIRING, group_key: '{"service":"api"}')
    change(monitor, IntegrationNotification::MONITOR_FIRING, group_key: "")

    IntegrationNotification.deliver_pending(@factory.id)

    deliveries = received
    assert_equal [ "solid-trace-monitor-#{monitor.id}-{\"service\":\"api\"}", "solid-trace-monitor-#{monitor.id}" ],
      deliveries.map { |d| d[:body]["dedupe_key"] }
    assert_equal Notification.order(:id).pluck(:id).map(&:to_s), deliveries.map { |d| d[:delivery] }

    grouped, single = deliveries.map { |d| d[:body] }
    assert_equal [ "shop", "shop" ], [ grouped["project"], single["project"] ]
    assert_equal "Firing: Checkout errors (service=api)", grouped["title"]
    assert_equal "Firing: Checkout errors", single["title"]
    assert_includes grouped["prompt"], "Checkout errors"
    assert_includes grouped["prompt"], "service=api"
    assert_match %r{\Ahttp://example.com/acme/}, grouped["link"]
    assert_equal({ "solidtrace.monitor_id" => monitor.id.to_s, "solidtrace.monitor_group" => "service=api",
                   "solidtrace.notification" => "monitor_firing" }, grouped["attributes"])
    assert_equal({ "solidtrace.monitor_id" => monitor.id.to_s, "solidtrace.notification" => "monitor_firing" },
      single["attributes"])
  end

  test "a Monitor's resolve starts no Run, and its row is skipped with the reason" do
    stub_monitor_query_check(@project)
    monitor = create(:monitor, project: @project, integrations: [ @factory ])
    change(monitor, IntegrationNotification::MONITOR_RESOLVED, group_key: "")

    IntegrationNotification.deliver_pending(@factory.id)

    assert_not_requested @webhook
    row = Notification.sole
    assert row.status_skipped?
    assert_match(/resolve/, row.error_message)
  end

  test "a 429, a 5xx and a connection failure are failed deliveries that later ticks retry" do
    [
      [ { status: 429, headers: { "Retry-After" => "30" }, body: { error: "too many" }.to_json }, /HTTP 429/ ],
      [ { status: 502, body: "bad gateway" }, /HTTP 502/ ],
      [ Errno::ECONNREFUSED, /ECONNREFUSED|Connection refused/ ],
    ].each do |answer, reason|
      Notification.delete_all
      answer.is_a?(Hash) ? stub_request(:post, WEBHOOK_URL).to_return(answer) : stub_request(:post, WEBHOOK_URL).to_raise(answer)
      IntegrationNotification.issue_received_event(create(:issue, project: @project), newly_created: true)

      IntegrationNotification.deliver_pending(@factory.id)

      row = Notification.sole
      assert row.status_failed?, "#{answer.inspect} is a failed delivery"
      assert_match reason, row.error_message
      assert row.retried?
    end
  end

  test "a retry sends the same delivery id, with a new timestamp and signature" do
    stub_request(:post, WEBHOOK_URL).to_return(status: 503, body: "down").then.to_return(accepted)
    IntegrationNotification.issue_received_event(create(:issue, project: @project), newly_created: true)
    IntegrationNotification.deliver_pending(@factory.id)
    travel IntegrationNotification::RATE_LIMIT_WINDOW

    IntegrationNotification.deliver_pending(@factory.id)

    deliveries = received
    assert_equal 2, deliveries.size
    assert_equal 1, deliveries.map { |d| d[:delivery] }.uniq.size
    assert_equal [ NOW.to_i, NOW.to_i + 60 ], deliveries.map { |d| d[:timestamp].to_i }
    assert Notification.sole.status_sent?
  end

  test "a 4xx other than 429 is refused: not retried, with the Dispatcher's reason" do
    [ 400, 401, 403, 404, 422 ].each do |status|
      Notification.delete_all
      stub_request(:post, WEBHOOK_URL).to_return(status: status, headers: { "Content-Type" => "application/json" },
        body: { error: "the Project shop has no Factory settings" }.to_json)
      IntegrationNotification.issue_received_event(create(:issue, project: @project), newly_created: true)

      IntegrationNotification.deliver_pending(@factory.id)

      row = Notification.sole
      assert row.status_refused?, "HTTP #{status} is refused"
      assert_equal "HTTP #{status}: the Project shop has no Factory settings", row.error_message
      assert_not row.retried?
      assert_equal row, Notification.last_failure(@factory)
    end
  end

  test "a refused Monitor row does not hold back the rest of its group's chain" do
    stub_monitor_query_check(@project)
    monitor = create(:monitor, project: @project, integrations: [ @factory ])
    change(monitor, IntegrationNotification::MONITOR_FIRING, group_key: "")
    change(monitor, IntegrationNotification::MONITOR_FIRING, group_key: "")
    stub_request(:post, WEBHOOK_URL).to_return(status: 422, body: { error: "no Factory settings" }.to_json)
      .then.to_return(accepted)

    IntegrationNotification.deliver_pending(@factory.id)

    assert_equal [ true, true ], Notification.order(:id).map { |row| row.status_refused? || row.status_sent? }
    assert Notification.order(:id).last.status_sent?
    assert_not Notification.order(:id).first.retried?
  end

  test "with the public webhook URL set, the provider sends there" do
    InstanceSetting.current.update!(dispatcher_webhook_url: "https://factory.example.com/hooks/")
    public_webhook = stub_request(:post, "https://factory.example.com/hooks/webhooks/generic/snd_1").to_return(accepted)
    IntegrationNotification.issue_received_event(create(:issue, project: @project), newly_created: true)

    IntegrationNotification.deliver_pending(@factory.id)

    assert_requested public_webhook
    assert_not_requested @webhook
    assert Notification.sole.status_sent?
  end

  test "with no Dispatcher URL, or no sender, the row is skipped and nothing is sent" do
    InstanceSetting.current.update!(dispatcher_url: nil)
    IntegrationNotification.issue_received_event(create(:issue, project: @project), newly_created: true)
    IntegrationNotification.deliver_pending(@factory.id)

    assert Notification.sole.status_skipped?
    assert_match(/Dispatcher URL/, Notification.sole.error_message)

    Notification.delete_all
    InstanceSetting.current.update!(dispatcher_url: DISPATCHER)
    @factory.update!(sender_id: nil)
    IntegrationNotification.issue_received_event(create(:issue, project: @project), newly_created: true)
    IntegrationNotification.deliver_pending(@factory.id)

    assert Notification.sole.status_skipped?
    assert_not_requested @webhook
  end

  test "the prompt and the title stay inside the webhook's limits" do
    issue = create(:issue, project: @project, title: "é" * 40_000, culprit: "x" * 70_000)
    IntegrationNotification.issue_received_event(issue, newly_created: true)

    IntegrationNotification.deliver_pending(@factory.id)

    body = received.sole[:body]
    assert_operator body["prompt"].bytesize, :<=, 65_536
    assert body["prompt"].valid_encoding?
    assert_operator body["title"].bytesize, :<=, 1024
    assert Notification.sole.status_sent?
  end

  test "the body's project is the Issue's Project, in the Integration's Organization" do
    other = create(:project, organization: @organization, name: "Billing")
    IntegrationNotification.issue_received_event(create(:issue, project: other), newly_created: true)

    IntegrationNotification.deliver_pending(@factory.id)

    assert_equal "billing", received.sole[:body]["project"]
  end

  private

  def accepted
    { status: 202, headers: { "Content-Type" => "application/json" },
      body: { run_id: "run_1", url: "http://localhost:3000/acme/runs/run_1", started: true }.to_json }
  end

  # Each request the Dispatcher got: its headers and parsed body, oldest first.
  def received
    signatures = []
    WebMock::RequestRegistry.instance.requested_signatures.each do |signature, count|
      signatures.concat([ signature ] * count) if signature.uri.to_s.include?("/webhooks/generic/")
    end
    signatures.map do |signature|
      { delivery: signature.headers["Solidtrace-Delivery"], timestamp: signature.headers["Solidtrace-Timestamp"],
        body: JSON.parse(signature.body) }
    end
  end

  def change(monitor, kind, group_key:)
    IntegrationNotification.monitor_changed(monitor, kind: kind, group_key: group_key, payload: {
      "value" => 12, "column" => "count_", "threshold" => 10.0, "operator" => ">",
      "from" => "2026-11-15T11:54:00Z", "to" => "2026-11-15T11:59:00Z",
    })
  end
end
