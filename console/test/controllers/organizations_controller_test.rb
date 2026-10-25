# frozen_string_literal: true

require "test_helper"

# A user in no Organization, such as one who just signed up or was archived
# from their only one, makes an Organization or is invited to one.
class OrganizationsControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  setup do
    @user = create(:user, email: "ada@example.com", password: "password")
  end

  test "a user in no Organization is sent from home to make one" do
    sign_in_as(@user)

    get root_url

    assert_redirected_to new_organization_url
  end

  test "a member archived from their only Organization is sent from home to make one" do
    create(:organization_user, user: @user).discard
    sign_in_as(@user)

    get root_url

    assert_redirected_to new_organization_url
  end

  test "a user in an Organization is still sent from home to its Issues" do
    organization = create(:organization)
    create(:organization_user, organization: organization, user: @user)
    sign_in_as(@user)

    get root_url

    assert_redirected_to overview_url(org_slug: organization.slug)
  end

  test "signing up leads to making an Organization, with the welcome kept" do
    InstanceSetting.current.update!(sign_up_open: true)
    post registration_url, params: { user: { name: "Grace", email: "grace@example.com", password: "password", password_confirmation: "password" } }
    follow_redirect!

    assert_redirected_to new_organization_url
    follow_redirect!
    assert_equal "Welcome! You have signed up successfully.", flash[:notice]
  end

  test "the page is an Inertia page, which renders in place of the sign-in form" do
    sign_in_as(@user)

    get new_organization_url, headers: inertia

    assert_response :success
    assert_equal "Organizations/New", response.parsed_body["component"]
  end

  test "making one makes the user its admin and opens its Issues" do
    sign_in_as(@user)

    post organizations_url, params: { organization: { name: "Acme" } }, as: :json, headers: inertia

    organization = Organization.find_by!(slug: "acme")
    assert_redirected_to overview_url(org_slug: "acme")
    assert organization.organization_users.find_by!(user: @user).admin?
  end

  test "a blank name comes back with its error" do
    sign_in_as(@user)

    assert_no_difference -> { Organization.count } do
      post organizations_url, params: { organization: { name: "" } }, as: :json, headers: inertia
    end

    assert_redirected_to new_organization_url
    get new_organization_url, headers: inertia
    assert_equal [ "can't be blank" ], response.parsed_body.dig("props", "errors", "name")
  end

  test "signed out, the page asks to sign in" do
    get new_organization_url

    assert_redirected_to login_url
  end

  private

  def inertia
    { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
  end
end
