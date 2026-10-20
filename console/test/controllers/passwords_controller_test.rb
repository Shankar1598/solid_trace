# frozen_string_literal: true

require "test_helper"

# "Forgot your password?": an email gets a link, the link opens a form for a
# new password, and the new password ends every session. Whether a user has
# the email is never said.
class PasswordsControllerTest < ActionDispatch::IntegrationTest
  include ActionMailer::TestHelper
  include IntegrationTestHelper

  SENT = "If a user has that email, it has been sent a link to reset the password."

  setup do
    @user = create(:user, email: "grace@example.com", password: "old-password")
  end

  test "Forgot your password? opens the form for an email" do
    get new_password_url, headers: inertia

    assert_response :success
    assert_equal "Passwords/New", response.parsed_body["component"]
  end

  test "a user's email, in any case, is sent a reset link" do
    assert_enqueued_email_with PasswordsMailer, :reset, args: [ @user ] do
      post passwords_url, params: { email: "Grace@Example.com" }, as: :json, headers: inertia
    end

    assert_redirected_to login_url
    assert_equal SENT, flash[:notice]
  end

  test "an email no user has is told the same, and sent nothing" do
    assert_no_enqueued_emails do
      post passwords_url, params: { email: "nobody@example.com" }, as: :json, headers: inertia
    end

    assert_redirected_to login_url
    assert_equal SENT, flash[:notice]
  end

  test "the link opens the form for a new password" do
    token = @user.password_reset_token
    get edit_password_url(token), headers: inertia

    assert_response :success
    assert_equal "Passwords/Edit", response.parsed_body["component"]
    assert_equal token, response.parsed_body.dig("props", "token")
  end

  test "a link that isn't a user's goes back to Forgot your password, saying why" do
    get edit_password_url("not-a-token"), headers: inertia

    assert_redirected_to new_password_url
    assert_equal "That reset link is wrong or has expired. Ask for another.", flash[:alert]
  end

  test "an expired link goes back to Forgot your password" do
    token = @user.password_reset_token

    travel 16.minutes do
      get edit_password_url(token), headers: inertia
    end

    assert_redirected_to new_password_url
  end

  test "the new password signs in, the old one doesn't, and every session ends" do
    sign_in_as(@user, password: "old-password")
    assert_equal 1, @user.sessions.count

    put password_url(@user.password_reset_token),
      params: { password: "new-password", password_confirmation: "new-password" }, as: :json, headers: inertia

    assert_redirected_to login_url
    assert_equal "Password reset. Sign in with the new one.", flash[:notice]
    assert_equal 0, @user.sessions.count
    assert_not User.authenticate_by(email: @user.email, password: "old-password")
    assert User.authenticate_by(email: @user.email, password: "new-password")
  end

  # A reset is how a user takes their account back, so whatever a stolen
  # session made goes too: an API token works without the password.
  test "a reset revokes the user's API tokens, so the MCP server refuses them" do
    _token, secret = ApiToken.issue!(user: @user, name: "Laptop")

    put password_url(@user.password_reset_token),
      params: { password: "new-password", password_confirmation: "new-password" }, as: :json, headers: inertia

    assert_equal 0, @user.api_tokens.count
    post mcp_url, params: { jsonrpc: "2.0", id: 1, method: "tools/list" }, as: :json,
      headers: { "Authorization" => "Bearer #{secret}", "Accept" => "application/json, text/event-stream" }
    assert_response :unauthorized
  end

  test "a reset makes an Invited user an ordinary one" do
    @user.update!(invited: true)

    put password_url(@user.password_reset_token),
      params: { password: "new-password", password_confirmation: "new-password" }, as: :json, headers: inertia

    assert_not @user.reload.invited?
  end

  test "a link works only until the password is reset" do
    token = @user.password_reset_token
    put password_url(token), params: { password: "new-password", password_confirmation: "new-password" }, as: :json, headers: inertia

    get edit_password_url(token), headers: inertia

    assert_redirected_to new_password_url
  end

  test "a confirmation that doesn't match is refused with its error, and the password stays" do
    token = @user.password_reset_token

    put password_url(token), params: { password: "new-password", password_confirmation: "other" }, as: :json, headers: inertia

    assert_redirected_to edit_password_url(token)
    assert User.authenticate_by(email: @user.email, password: "old-password")
    get edit_password_url(token), headers: inertia
    assert_equal [ "doesn't match Password" ], response.parsed_body.dig("props", "errors", "password_confirmation")
  end

  test "a blank password is refused" do
    token = @user.password_reset_token

    put password_url(token), params: { password: "", password_confirmation: "" }, as: :json, headers: inertia

    assert_redirected_to edit_password_url(token)
    assert User.authenticate_by(email: @user.email, password: "old-password")
  end

  private

  def inertia
    { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
  end
end
