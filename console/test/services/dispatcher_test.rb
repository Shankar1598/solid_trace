# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# Console's calls to the Dispatcher's private API, as later pages make them
# (the Stop button, the Factory section): the body goes as JSON with
# Console's factory token, and each failure is an error with a clear message.
class DispatcherTest < ActiveSupport::TestCase
  DISPATCHER = "http://10.100.0.3:8080/factory"

  setup do
    @organization = create(:organization, slug: "acme")
    @user = create(:user)
    @organization.organization_users.create!(user: @user, role: :member)
    InstanceSetting.current.update!(dispatcher_url: DISPATCHER)
    @dispatcher = Dispatcher.new(user: @user, organization: @organization)
  end

  test "a call goes under the URL's path, with the body as JSON and Console's factory token, and answers the JSON" do
    stop = stub_request(:post, "#{DISPATCHER}/api/v1/runs/r-1/stop")
      .with(body: { reason: "Stopped from Console" }.to_json,
            headers: { "Content-Type" => "application/json", "Accept" => "application/json" })
      .to_return(status: 200, body: { id: "r-1", state: "stopping" }.to_json)

    answer = @dispatcher.request(:post, "/api/v1/runs/r-1/stop", body: { reason: "Stopped from Console" })

    assert_equal({ "id" => "r-1", "state" => "stopping" }, answer)
    assert_requested(stop)
    assert_requested(:post, "#{DISPATCHER}/api/v1/runs/r-1/stop") do |request|
      token = Oauth::ConsoleToken.find_live(request.headers["Authorization"].delete_prefix("Bearer "))
      token && token.user == @user && token.organization == @organization
    end
  end

  test "an error status raises Refused with the Dispatcher's error text and status" do
    stub_request(:get, "#{DISPATCHER}/api/v1/projects")
      .to_return(status: 403, body: { error: "only an Admin of the Organization acme can do this" }.to_json)

    error = assert_raises(Dispatcher::Refused) { @dispatcher.request(:get, "/api/v1/projects") }

    assert_equal 403, error.status
    assert_equal "only an Admin of the Organization acme can do this", error.message
  end

  test "a 503 status answer is an error for any call but the status" do
    stub_request(:get, "#{DISPATCHER}/api/v1/runs").to_return(status: 503, body: { error: "down" }.to_json)

    assert_raises(Dispatcher::Refused) { @dispatcher.request(:get, "/api/v1/runs") }
  end

  test "with no Dispatcher URL a call raises Unavailable and sends nothing" do
    InstanceSetting.current.update!(dispatcher_url: nil)
    any = stub_request(:any, /.*/)

    error = assert_raises(Dispatcher::Unavailable) do
      Dispatcher.new(user: @user, organization: @organization).request(:get, "/api/v1/runs")
    end

    assert_equal "No Dispatcher URL is set in Settings → Instance.", error.message
    assert_not_requested any
  end

  test "Console makes no token for an Organization that the user is not in, and sends nothing" do
    other = create(:organization)
    any = stub_request(:any, /.*/)

    assert_raises(ArgumentError) { Dispatcher.new(user: @user, organization: other).request(:get, "/api/v1/runs") }
    assert_not_requested any
  end
end
