# frozen_string_literal: true

require "test_helper"

# On a fresh VM install, the Meta Organization is seeded before anyone signs
# up, and the person who set up the instance signs up first (ADR 0011).
class RegistrationsControllerTest < ActionDispatch::IntegrationTest
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

    sign_up("ada@example.com")

    assert_equal [ "grace@example.com" ], Organization.find_by!(name: "SolidTrace").users.pluck(:email)
  end

  test "without a Meta Organization, signing up joins no Organization" do
    sign_up("grace@example.com")

    assert_empty User.find_by!(email: "grace@example.com").organizations
  end

  private

  def sign_up(email)
    post registration_url, params: { user: { name: "Someone", email: email, password: "password", password_confirmation: "password" } }
  end
end
