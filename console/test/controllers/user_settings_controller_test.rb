# frozen_string_literal: true

require "test_helper"

class UserSettingsControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  setup do
    @organization = create(:organization)
    @user = create(:user, name: "Ada")
    @organization.users << @user

    sign_in_as(@user)
  end

  # The page's form sends its fields nested under user, as a JSON body.
  test "the profile form saves the name from User settings" do
    put user_settings_url, params: { user: { name: "Ada Lovelace", email: @user.email } }, as: :json, headers: inertia

    assert_redirected_to user_settings_path
    assert_equal "Ada Lovelace", @user.reload.name
  end

  test "the profile form saves the name from the Organization's User settings" do
    put user_path(org_slug: @organization.slug), params: { user: { name: "Ada Lovelace", email: @user.email } }, as: :json, headers: inertia

    assert_redirected_to user_path(org_slug: @organization.slug)
    assert_equal "Ada Lovelace", @user.reload.name
  end

  test "the profile form can't change the email" do
    email = @user.email
    put user_settings_url, params: { user: { name: "Ada", email: "someone-else@example.com" } }, as: :json, headers: inertia

    assert_equal email, @user.reload.email
  end

  test "a blank name is refused with its error" do
    put user_settings_url, params: { user: { name: "", email: @user.email } }, as: :json, headers: inertia

    assert_equal "Ada", @user.reload.name
    get user_settings_url, headers: inertia.merge("X-Inertia-Version" => ViteRuby.digest)
    assert_equal "Settings/User", response.parsed_body["component"]
    assert_equal [ "can't be blank" ], response.parsed_body.dig("props", "errors", "name")
  end

  private

  def inertia
    { "X-Inertia" => "true" }
  end
end
