# frozen_string_literal: true

require "test_helper"

# On a fresh VM install, the Meta Organization is seeded before anyone signs
# up, and the person who set up the instance signs up first (ADR 0011). After
# that, sign-up is closed unless the operator opens it: people join by Invite.
class RegistrationsControllerTest < ActionDispatch::IntegrationTest
  CLOSED = "Ask an admin of your Organization to invite you."

  test "while the Meta Organization has no member, the first to sign up joins it as admin" do
    MetaOrganization.seed("0123456789abcdef0123456789abcdef")

    sign_up("grace@example.com")

    membership = Organization.find_by!(name: "SolidTrace").organization_users.sole
    assert_equal "grace@example.com", membership.user.email
    assert membership.admin?
  end

  test "a later sign-up does not join it" do
    MetaOrganization.seed("0123456789abcdef0123456789abcdef")
    sign_up("grace@example.com")
    delete session_url
    InstanceSetting.current.update!(sign_up_open: true)

    sign_up("ada@example.com")

    assert User.exists?(email: "ada@example.com")
    assert_equal [ "grace@example.com" ], Organization.find_by!(name: "SolidTrace").users.pluck(:email)
  end

  test "the first to sign up is the instance admin, and a later sign-up isn't" do
    sign_up("grace@example.com")
    delete session_url
    InstanceSetting.current.update!(sign_up_open: true)

    sign_up("ada@example.com")

    assert User.find_by!(email: "grace@example.com").instance_admin?
    assert_not User.find_by!(email: "ada@example.com").instance_admin?
  end

  test "without a Meta Organization, signing up joins no Organization" do
    sign_up("grace@example.com")

    assert_empty User.find_by!(email: "grace@example.com").organizations
  end

  test "while the install has no user, the page is the sign-up form" do
    get new_registration_url, headers: inertia

    assert_response :success
    assert_equal "Registrations/New", response.parsed_body["component"]
    assert_nil response.parsed_body.dig("props", "closed")
  end

  test "once the install has a user, the page says sign-up is closed" do
    create(:user)

    get new_registration_url, headers: inertia

    assert_response :success
    assert_equal "Registrations/New", response.parsed_body["component"]
    assert_equal CLOSED, response.parsed_body.dig("props", "closed")
  end

  test "once the install has a user, signing up anyway makes no user" do
    create(:user)

    assert_no_difference -> { User.count } do
      sign_up("grace@example.com")
    end
    assert_redirected_to new_registration_url
    assert_nil cookies[:session_id].presence
  end

  test "with sign-up opened by the operator, anyone signs up" do
    create(:user)
    InstanceSetting.current.update!(sign_up_open: true)

    sign_up("grace@example.com")

    assert_redirected_to root_url
    assert User.exists?(email: "grace@example.com")
  end

  test "too many tries are sent back to the form, told to wait" do
    RegistrationsController.cache_store.stub(:increment, 11) do
      sign_up("grace@example.com")
    end

    assert_redirected_to new_registration_url
    assert_equal "Too many tries. Try again in a few minutes.", flash[:alert]
    assert_not User.exists?(email: "grace@example.com")
  end

  private

  def sign_up(email)
    post registration_url, params: { user: { name: "Someone", email: email, password: "password", password_confirmation: "password" } }
  end

  def inertia
    { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
  end
end
