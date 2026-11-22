# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# The 'Factory' Integration provider (W6), as an Admin and a member see it
# on the Integrations pages: it shows only when the Dispatcher URL is set,
# has no URL field, and makes and deletes its Webhook sender through the
# Dispatcher's API as the signed-in Admin.
class FactoryIntegrationTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper
  include TwoFactorTestHelper

  DISPATCHER = "http://10.100.0.3:8080"
  SENDERS_URL = "#{DISPATCHER}/api/v1/senders"
  SECRET = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"

  setup do
    stub_request(:get, %r{/config\z}).to_return(status: 503)
    @organization = create(:organization)
    @admin = create(:user)
    @organization.organization_users.create!(user: @admin, role: :admin)
    @member = create(:user)
    @organization.organization_users.create!(user: @member, role: :member)
    InstanceSetting.current.update!(dispatcher_url: DISPATCHER)
  end

  test "the provider shows only when the Dispatcher URL is set" do
    sign_in_as(@admin)

    assert_includes new_page.dig("props", "providers").keys, "factory"
    assert_equal "Factory", new_page.dig("props", "providers", "factory")

    InstanceSetting.current.update!(dispatcher_url: nil)

    assert_not_includes new_page.dig("props", "providers").keys, "factory"
  end

  test "a member's form offers no Factory provider, which only an Admin makes" do
    sign_in_as(@member)

    providers = new_page.dig("props", "providers").keys
    assert_not_includes providers, "factory"
    assert_includes providers, "slack"

    sign_in_as(@admin)

    assert_includes new_page.dig("props", "providers").keys, "factory"
  end

  test "with no Dispatcher URL a Factory Integration is refused, with no call to a Dispatcher" do
    InstanceSetting.current.update!(dispatcher_url: nil)
    sign_in_as(@admin)

    assert_no_difference -> { Integration.count } do
      post integrations_url(org_slug: @organization.slug), params: { integration: { provider: "factory" } }
    end

    assert_equal [ "needs the Dispatcher URL, which an Instance admin sets in Settings → Instance" ],
      inertia_errors["provider"]
    assert_not_requested :any, //
  end

  test "when the record does not save after the sender is made, Console deletes that sender at the Dispatcher" do
    stub_create_sender
    delete_sender = stub_request(:delete, "#{SENDERS_URL}/snd_1").to_return(status: 204)
    sign_in_as(@admin)

    with_failed_save do
      assert_no_difference -> { Integration.count } do
        post integrations_url(org_slug: @organization.slug), params: { integration: { provider: "factory" } }
      end
    end

    assert_requested(:post, SENDERS_URL, times: 1)
    assert_requested delete_sender
    assert_equal [ "the database refused it" ], inertia_errors["base"]
  end

  test "an Admin makes a Factory Integration: Console makes its sender as that Admin and keeps the secret encrypted" do
    stub_create_sender
    sign_in_as(@admin)

    post integrations_url(org_slug: @organization.slug), params: { integration: { provider: "factory", name: "Fix bugs",
      settings: { notify_on_new_issue: "1" } } }

    assert_redirected_to integrations_url(org_slug: @organization.slug)
    integration = @organization.integrations.sole
    assert_equal [ "factory", "Fix bugs", "snd_1" ], [ integration.provider, integration.name, integration.sender_id ]
    assert_equal SECRET, integration.sender_secret
    # No list of Projects: the sender can start Runs in each Project of the
    # Organization of the Admin's token, and no other. It is the Factory
    # Integration's sender, so only its deliveries start Issue and Monitor
    # Runs.
    assert_requested(:post, SENDERS_URL, times: 1) do |request|
      assert_equal({ "name" => "Fix bugs", "factory" => true }, JSON.parse(request.body))
      token = Oauth::ConsoleToken.find_live(request.headers["Authorization"].delete_prefix("Bearer "))
      assert_equal [ @admin, @organization ], [ token.user, token.organization ]
    end

    stored = Integration.connection.select_value("SELECT sender_secret FROM integrations WHERE id = #{integration.id}")
    assert_not_includes stored, SECRET, "the secret is stored encrypted"

    get edit_integration_url(integration, org_slug: @organization.slug), headers: inertia
    assert_response :success
    assert_not_includes response.body, SECRET, "no page shows the secret"
    assert_not_includes response.body, "snd_1"
  end

  test "a Factory Integration keeps no URL that the form sends" do
    stub_create_sender
    sign_in_as(@admin)

    post integrations_url(org_slug: @organization.slug), params: { integration: { provider: "factory",
      settings: { webhook_url: "http://198.51.100.92/hook", routing_key: "k", recipients: "a@example.com" } } }

    integration = @organization.integrations.sole
    assert_equal "Factory", integration.name
    assert_nil integration.webhook_url
    assert_empty integration.settings.slice("webhook_url", "routing_key", "recipients")
  end

  test "a member cannot make, change or delete a Factory Integration, and Console calls no Dispatcher" do
    integration = factory_integration
    sign_in_as(@member)

    assert_no_difference -> { Integration.count } do
      post integrations_url(org_slug: @organization.slug), params: { integration: { provider: "factory" } }
    end
    assert_response :forbidden

    patch integration_url(integration, org_slug: @organization.slug), params: { integration: { name: "Mine" } }
    assert_response :forbidden
    assert_equal "Factory", integration.reload.name

    delete integration_url(integration, org_slug: @organization.slug)
    assert_response :forbidden
    assert Integration.exists?(integration.id)
    assert_not_requested :any, /10\.100\.0\.3/
  end

  test "when the Dispatcher refuses the sender, or is down, no Integration is made and the reason shows" do
    sign_in_as(@admin)

    stub_request(:post, SENDERS_URL).to_return(json({ error: "only an Admin of this Organization does this" }, status: 403))
    assert_no_difference -> { Integration.count } do
      post integrations_url(org_slug: @organization.slug), params: { integration: { provider: "factory" } }
    end
    assert_match(/only an Admin of this Organization does this/, inertia_errors["provider"].first)

    stub_request(:post, SENDERS_URL).to_raise(Errno::ECONNREFUSED)
    assert_no_difference -> { Integration.count } do
      post integrations_url(org_slug: @organization.slug), params: { integration: { provider: "factory" } }
    end
    assert_match(/could not connect to the Dispatcher/, inertia_errors["provider"].first)
  end

  test "an answer with no sender id or no secret makes no Integration" do
    sign_in_as(@admin)
    stub_request(:post, SENDERS_URL).to_return(json({ id: "snd_1", name: "Factory" }, status: 201))

    assert_no_difference -> { Integration.count } do
      post integrations_url(org_slug: @organization.slug), params: { integration: { provider: "factory" } }
    end
    assert_match(/no sender/, inertia_errors["provider"].first)
  end

  test "deleting a Factory Integration deletes its sender at the Dispatcher" do
    integration = factory_integration
    delete_sender = stub_request(:delete, "#{SENDERS_URL}/snd_1").to_return(status: 204)
    sign_in_as(@admin)

    delete integration_url(integration, org_slug: @organization.slug)

    assert_redirected_to integrations_url(org_slug: @organization.slug)
    assert_equal "Integration deleted.", flash[:notice]
    assert_nil flash[:alert]
    assert_requested delete_sender
    assert_not Integration.exists?(integration.id)
  end

  test "a Factory Integration with sent, pending and failed deliveries is deleted with its rows and its sender" do
    integration = factory_integration
    issue = create(:issue, project: create(:project, organization: @organization))
    rows = 3.times.map { Notification.enqueue!(integration: integration, issue: issue, kind: IntegrationNotification::ISSUE_CREATED) }
    Notification.mark_rows([ rows[0].id ], :sent)
    Notification.mark_rows([ rows[2].id ], :failed, "HTTP 503")
    delete_sender = stub_request(:delete, "#{SENDERS_URL}/snd_1").to_return(status: 204)
    sign_in_as(@admin)

    delete integration_url(integration, org_slug: @organization.slug)

    assert_redirected_to integrations_url(org_slug: @organization.slug)
    assert_requested delete_sender
    assert_not Integration.exists?(integration.id)
    assert_not Notification.exists?(integration_id: integration.id)
  end

  test "a sender that the Dispatcher no longer has: Console deletes the Integration with no warning" do
    integration = factory_integration
    stub_request(:delete, "#{SENDERS_URL}/snd_1").to_return(json({ error: "no sender snd_1 in this Organization" }, status: 404))
    sign_in_as(@admin)

    delete integration_url(integration, org_slug: @organization.slug)

    assert_not Integration.exists?(integration.id)
    assert_nil flash[:alert]
  end

  test "with the Dispatcher down, Console deletes its record and warns that the sender stays" do
    integration = factory_integration
    stub_request(:delete, "#{SENDERS_URL}/snd_1").to_raise(Errno::ECONNREFUSED)
    sign_in_as(@admin)

    delete integration_url(integration, org_slug: @organization.slug)

    assert_redirected_to integrations_url(org_slug: @organization.slug)
    assert_not Integration.exists?(integration.id)
    assert_match(/could not delete its sender snd_1 at the Dispatcher/, flash[:alert])
    assert_match(/solidtrace senders delete snd_1/, flash[:alert])
  end

  test "with no Dispatcher URL any more, deleting the Integration warns that the sender stays" do
    integration = factory_integration
    InstanceSetting.current.update!(dispatcher_url: nil)
    sign_in_as(@admin)

    delete integration_url(integration, org_slug: @organization.slug)

    assert_not Integration.exists?(integration.id)
    assert_match(/snd_1/, flash[:alert])
  end

  test "an Admin changes a Factory Integration's name and rules, with no call to the Dispatcher" do
    integration = factory_integration
    sign_in_as(@admin)

    patch integration_url(integration, org_slug: @organization.slug), params: { integration: { name: "Triage",
      settings: { notify_on_assignment: "1", webhook_url: "http://198.51.100.92/hook" } } }

    assert_redirected_to integrations_url(org_slug: @organization.slug)
    integration.reload
    assert_equal [ "Triage", true, nil ], [ integration.name, integration.notify_on_assignment, integration.webhook_url ]
    assert_equal [ "snd_1", SECRET ], [ integration.sender_id, integration.sender_secret ]
    assert_not_requested :any, /10\.100\.0\.3/
  end

  test "an update does not change an Integration's provider" do
    slack = @organization.integrations.create!(provider: "slack", settings: { "webhook_url" => "https://hooks.slack.com/x" })
    integration = factory_integration
    sign_in_as(@member)

    patch integration_url(slack, org_slug: @organization.slug), params: { integration: { provider: "factory" } }
    assert_equal "slack", slack.reload.provider

    sign_in_as(@admin)
    patch integration_url(integration, org_slug: @organization.slug), params: { integration: { provider: "slack",
      settings: { webhook_url: "https://hooks.slack.com/x" } } }
    assert_equal "factory", integration.reload.provider
  end

  test "an Integration's page shows its last failed delivery with the reason" do
    integration = factory_integration
    issue = create(:issue, project: create(:project, organization: @organization))
    sent = Notification.enqueue!(integration: integration, issue: issue, kind: IntegrationNotification::ISSUE_CREATED)
    Notification.mark_rows([ sent.id ], :sent)
    refused = Notification.enqueue!(integration: integration, issue: issue, kind: IntegrationNotification::ISSUE_CREATED)
    Notification.mark_rows([ refused.id ], :refused, "HTTP 422: the Project shop has no Factory settings")
    sign_in_as(@member)

    get edit_integration_url(integration, org_slug: @organization.slug), headers: inertia

    failure = response.parsed_body.dig("props", "integration", "last_failure")
    assert_equal "HTTP 422: the Project shop has no Factory settings", failure["reason"]
    assert_equal false, failure["retried"]
    assert failure["at"].present?
  end

  test "an Integration's page shows no failure when a later delivery was sent" do
    integration = factory_integration
    issue = create(:issue, project: create(:project, organization: @organization))
    refused = Notification.enqueue!(integration: integration, issue: issue, kind: IntegrationNotification::ISSUE_CREATED)
    Notification.mark_rows([ refused.id ], :refused, "HTTP 422: the Project shop has no Factory settings")
    travel 1.hour do
      sent = Notification.enqueue!(integration: integration, issue: issue, kind: IntegrationNotification::ISSUE_CREATED)
      Notification.mark_rows([ sent.id ], :sent)
      sign_in_as(@member)

      get edit_integration_url(integration, org_slug: @organization.slug), headers: inertia

      assert_nil response.parsed_body.dig("props", "integration", "last_failure")
    end
  end

  private

  def new_page
    get new_integration_url(org_slug: @organization.slug), headers: inertia
    assert_response :success
    response.parsed_body
  end

  def stub_create_sender
    stub_request(:post, SENDERS_URL).to_return do |request|
      name = JSON.parse(request.body)["name"]
      json({ id: "snd_1", name: name, projects: [], created_at: "2026-11-15T12:00:00Z",
             created_by: { id: @admin.id.to_s, name: @admin.name }, secret: SECRET }, status: 201)
    end
  end

  # Each Integration that Console builds in the block refuses its save, as a
  # record that a validation refuses after the Dispatcher made its sender.
  def with_failed_save(&block)
    build = Integration.method(:new)
    failing = lambda do |*args, **options, &init|
      build.call(*args, **options, &init).tap do |integration|
        integration.define_singleton_method(:save) do |*|
          errors.add(:base, "the database refused it")
          false
        end
      end
    end
    Integration.stub(:new, failing, &block)
  end

  def factory_integration
    @organization.integrations.create!(provider: "factory", sender_id: "snd_1", sender_secret: SECRET)
  end

  def json(body, status: 200)
    { status: status, headers: { "Content-Type" => "application/json" }, body: body.to_json }
  end

  def inertia_errors
    follow_redirect!(headers: inertia) if response.redirect?
    JSON.parse(response.body)["props"]["errors"] || {}
  end
end
