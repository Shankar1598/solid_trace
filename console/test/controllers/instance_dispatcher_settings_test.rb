# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# Settings → Instance: the Dispatcher's private API URL. Only an Instance
# admin sets it. With none, Console shows no part of the Factory that needs
# the Dispatcher; with one, the page shows the Dispatcher's status, or a
# clear error when the Dispatcher does not answer.
class InstanceDispatcherSettingsTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper
  include TwoFactorTestHelper

  DISPATCHER = "http://10.100.0.3:8080"
  STATUS_URL = "#{DISPATCHER}/api/v1/status"

  setup do
    stub_request(:get, %r{/config\z}).to_return(status: 503)
    @organization = create(:organization)
    @instance_admin = create(:user, instance_admin: true)
    @organization.organization_users.create!(user: @instance_admin, role: :admin)
    @org_admin = create(:user)
    @organization.organization_users.create!(user: @org_admin, role: :admin)
  end

  test "an Instance admin saves the Dispatcher URL, and the page and every page's factory_on show it" do
    sign_in_as(@instance_admin)

    patch instance_settings_url(org_slug: @organization.slug),
      params: { instance_settings: { dispatcher_url: " http://10.100.0.3:8080/ " } }

    assert_redirected_to instance_settings_url(org_slug: @organization.slug)
    page = settings_page
    assert_equal DISPATCHER, page.dig("props", "settings", "editable", "dispatcher_url")
    assert_equal true, page.dig("props", "factory_on")
    get api_token_settings_url(org_slug: @organization.slug), headers: inertia
    assert_equal true, response.parsed_body.dig("props", "factory_on")
  end

  test "a URL with a path saves; a URL that is not http(s), or has a user or a query, comes back as a form error" do
    sign_in_as(@instance_admin)

    patch instance_settings_url(org_slug: @organization.slug),
      params: { instance_settings: { dispatcher_url: "https://factory.example.com/dispatcher" } }
    assert_equal "https://factory.example.com/dispatcher", InstanceSetting.current.dispatcher_url

    [ "ftp://10.100.0.3", "10.100.0.3:8080", "http://admin:secret@10.100.0.3:8080", "http://10.100.0.3:8080/?a=1",
      "http://" ].each do |bad|
      patch instance_settings_url(org_slug: @organization.slug), params: { instance_settings: { dispatcher_url: bad } },
        headers: inertia
      follow_redirect!(headers: inertia)
      assert_match "must be http:// or https://", response.parsed_body.dig("props", "errors", "dispatcher_url").to_s, bad
      assert_equal "https://factory.example.com/dispatcher", InstanceSetting.current.dispatcher_url, bad
    end
  end

  test "an Organization admin who is not an Instance admin gets 403 and changes nothing" do
    InstanceSetting.current.update!(dispatcher_url: DISPATCHER)
    sign_in_as(@org_admin)

    patch instance_settings_url(org_slug: @organization.slug),
      params: { instance_settings: { dispatcher_url: "http://198.51.100.92:8080" } }

    assert_response :forbidden
    assert_equal DISPATCHER, InstanceSetting.current.dispatcher_url
  end

  test "a member gets 403 and changes nothing, also when clearing it" do
    InstanceSetting.current.update!(dispatcher_url: DISPATCHER)
    member = create(:user)
    @organization.organization_users.create!(user: member, role: :member)
    sign_in_as(member)

    patch instance_settings_url(org_slug: @organization.slug), params: { instance_settings: { dispatcher_url: "" } }

    assert_response :forbidden
    assert_equal DISPATCHER, InstanceSetting.current.dispatcher_url
  end

  test "with no Dispatcher URL, no factory part shows and Console calls no Dispatcher" do
    calls = stub_request(:any, /.*:8080/)
    sign_in_as(@instance_admin)

    page = settings_page

    assert_nil page.dig("props", "settings", "editable", "dispatcher_url")
    assert_nil page.dig("props", "dispatcher")
    assert_nil page.dig("deferredProps")
    assert_equal false, page.dig("props", "factory_on")
    assert_nil dispatcher_prop
    assert_not_requested calls
  end

  test "clearing the Dispatcher URL hides the factory parts again" do
    InstanceSetting.current.update!(dispatcher_url: DISPATCHER)
    sign_in_as(@instance_admin)

    patch instance_settings_url(org_slug: @organization.slug), params: { instance_settings: { dispatcher_url: "" } }

    assert_nil InstanceSetting.current.dispatcher_url
    assert_equal false, settings_page.dig("props", "factory_on")
  end

  test "the page shows at once, and loads the Dispatcher's status after it shows" do
    InstanceSetting.current.update!(dispatcher_url: DISPATCHER)
    status = stub_request(:get, STATUS_URL).to_return(status: 200, headers: { "Content-Type" => "application/json" },
      body: { version: "v0.3.0", solidtrace: { url: "https://console.example.com", token_active: true } }.to_json)
    sign_in_as(@instance_admin)

    page = settings_page
    assert_not page["props"].key?("dispatcher")
    assert_equal [ "dispatcher" ], page.dig("deferredProps", "default")
    assert_not_requested status

    assert_equal({ "version" => "v0.3.0", "token_active" => true }, dispatcher_prop)
  end

  test "a Dispatcher whose own token SolidTrace refuses shows that error" do
    InstanceSetting.current.update!(dispatcher_url: DISPATCHER)
    stub_request(:get, STATUS_URL).to_return(status: 503, body: {
      version: "v0.3.0", solidtrace: { url: "https://console.example.com", token_active: false,
                                       error: "SolidTrace refused the Dispatcher's token: the Admin left" },
    }.to_json)
    sign_in_as(@instance_admin)

    assert_equal({ "version" => "v0.3.0", "token_active" => false,
                   "token_error" => "SolidTrace refused the Dispatcher's token: the Admin left" }, dispatcher_prop)
  end

  test "a Dispatcher that does not answer gives a clear error on the page, not a 500" do
    InstanceSetting.current.update!(dispatcher_url: DISPATCHER)
    sign_in_as(@instance_admin)
    {
      -> { stub_request(:get, STATUS_URL).to_raise(Errno::ECONNREFUSED) } => "Console could not connect to the Dispatcher at #{DISPATCHER}",
      -> { stub_request(:get, STATUS_URL).to_raise(SocketError.new("getaddrinfo: Name or service not known")) } =>
        "Console could not connect to the Dispatcher at #{DISPATCHER}",
      -> { stub_request(:get, STATUS_URL).to_timeout } => "The Dispatcher at #{DISPATCHER} did not answer in time",
      -> { stub_request(:get, STATUS_URL).to_return(status: 502, body: "<html>Bad Gateway</html>") } =>
        "The Dispatcher at #{DISPATCHER} answered HTTP 502 with no JSON",
      -> { stub_request(:get, STATUS_URL).to_return(status: 200, body: "OK") } =>
        "The Dispatcher at #{DISPATCHER} answered HTTP 200 with no JSON",
      -> { stub_request(:get, STATUS_URL).to_return(status: 404, body: { error: "not found" }.to_json) } => "not found",
      -> { stub_request(:get, STATUS_URL).to_return(status: 500, body: "{}") } =>
        "The Dispatcher refused the request (HTTP 500).",
    }.each do |stub, message|
      WebMock.reset!
      stub_request(:get, %r{/config\z}).to_return(status: 503)
      stub.call

      error = dispatcher_prop&.fetch("error", nil)

      assert_response :success
      assert error.to_s.start_with?(message), "#{message.inspect} not at the start of #{error.inspect}"
    end
  end

  test "the full page shows also while the Dispatcher does not answer" do
    InstanceSetting.current.update!(dispatcher_url: DISPATCHER)
    stub_request(:get, STATUS_URL).to_timeout
    sign_in_as(@instance_admin)

    get instance_settings_url(org_slug: @organization.slug)

    assert_response :success
  end

  test "an Organization admin who is not an Instance admin cannot load the Dispatcher's status" do
    InstanceSetting.current.update!(dispatcher_url: DISPATCHER)
    status = stub_request(:get, STATUS_URL)
    sign_in_as(@org_admin)

    get instance_settings_url(org_slug: @organization.slug), headers: partial_headers

    assert_redirected_to root_url
    assert_not_requested status
  end

  private

  def settings_page
    get instance_settings_url(org_slug: @organization.slug), headers: inertia
    assert_response :success
    response.parsed_body
  end

  # The deferred dispatcher prop, as the page loads it after it shows.
  def dispatcher_prop
    get instance_settings_url(org_slug: @organization.slug), headers: partial_headers
    assert_response :success
    response.parsed_body.dig("props", "dispatcher")
  end

  def partial_headers
    inertia.merge("X-Inertia-Partial-Component" => "Settings/Instance", "X-Inertia-Partial-Data" => "dispatcher")
  end
end
