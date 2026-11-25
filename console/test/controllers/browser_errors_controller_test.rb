# frozen_string_literal: true

require "test_helper"
require "sentry/test_helper"

# The frontend's errors reach Console's Sentry, the Meta Project on an
# install, through Console: a signed-in browser's envelope is sent on as it
# is, with Console's DSN, while Send Console's Issues is on (docs/traces.md).
class BrowserErrorsControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper
  include Sentry::TestHelper

  ENVELOPE = %({"event_id":"0123456789abcdef0123456789abcdef","dsn":"http://browser@www.example.com/0"}\n) +
    %({"type":"event"}\n{"exception":{"values":[{"type":"Error","value":"Base UI error #85"}]}}\n)

  setup do
    ConsoleSentry.reset
    @main_client = Sentry.get_main_hub.current_client
    setup_sentry_test
    # Each request's Sentry middleware copies the main hub, so the requests
    # get the test's client too.
    Sentry.get_main_hub.bind_client(Sentry.get_current_client)
    @user = create(:user)
    create(:organization).users << @user
    @sent = []
  end

  teardown do
    teardown_sentry_test
    Sentry.get_main_hub.bind_client(@main_client)
    ConsoleSentry.reset
  end

  test "a signed-in browser's envelope is sent on as it is, and the frontend is told to send its errors" do
    sign_in_as(@user)
    sending { post browser_errors_url, params: ENVELOPE, headers: { "Content-Type" => "text/plain;charset=UTF-8" } }

    assert_response :accepted
    assert_equal [ ENVELOPE ], @sent

    get user_settings_url, headers: inertia_headers
    errors = response.parsed_body.dig("props", "browser_errors")
    assert_equal Sentry.configuration.environment, errors["environment"]
    assert errors.key?("release")
  end

  test "with Send Console's Issues off, an envelope is not sent and the frontend sends none" do
    InstanceSetting.current.update!(send_console_issues: false)
    sign_in_as(@user)
    sending { post browser_errors_url, params: ENVELOPE }

    assert_response :no_content
    assert_empty @sent

    get user_settings_url, headers: inertia_headers
    assert_nil response.parsed_body.dig("props", "browser_errors")
  end

  test "a signed-out browser's envelope is not sent" do
    sending { post browser_errors_url, params: ENVELOPE }

    assert_response :redirect
    assert_empty @sent
  end

  test "an envelope over a megabyte is refused" do
    sign_in_as(@user)
    sending { post browser_errors_url, params: "x" * (BrowserErrorsController::MAX_BYTES + 1) }

    assert_response :content_too_large
    assert_empty @sent
  end

  test "an envelope Console's Sentry refuses answers 502" do
    sign_in_as(@user)
    Sentry.get_current_client.transport.stub(:send_data, ->(_) { raise Sentry::ExternalError, "status 500" }) do
      post browser_errors_url, params: ENVELOPE
    end

    assert_response :bad_gateway
  end

  private

  def sending(&block)
    Sentry.get_current_client.transport.stub(:send_data, ->(data) { @sent << data }, &block)
  end

  def inertia_headers
    { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
  end
end
