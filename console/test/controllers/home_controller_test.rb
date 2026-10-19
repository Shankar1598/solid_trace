# frozen_string_literal: true

require "test_helper"

# `/` opens the Organization the user last opened, while they are still in it,
# and otherwise their first Organization by name.
class HomeControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  setup do
    @user = create(:user, email: "ada@example.com", password: "password")
    @acme = create(:organization, name: "Acme")
    @meta = create(:organization, name: "Meta")
    create(:organization_user, organization: @meta, user: @user)
    create(:organization_user, organization: @acme, user: @user)
    sign_in_as(@user)
  end

  test "home opens the Organization last opened" do
    get projects_url(org_slug: @meta.slug)

    get root_url

    assert_redirected_to issues_url(org_slug: @meta.slug)
  end

  test "any page of an Organization counts as opening it" do
    get organization_url(org_slug: @meta.slug)
    get user_url(org_slug: @meta.slug)

    get root_url

    assert_redirected_to issues_url(org_slug: @meta.slug)
  end

  test "home opens the first Organization by name before any is opened" do
    zeta = create(:organization, name: "Zeta")
    create(:organization_user, organization: zeta, user: @user)

    get root_url

    assert_redirected_to issues_url(org_slug: @acme.slug)
  end

  test "a user removed from the last used Organization is sent to their first by name" do
    get projects_url(org_slug: @meta.slug)
    @meta.organization_users.find_by!(user: @user).discard

    get root_url

    assert_redirected_to issues_url(org_slug: @acme.slug)
  end

  test "a user whose last used Organization was deleted is sent to their first by name" do
    get projects_url(org_slug: @meta.slug)
    OrganizationUser.where(organization: @meta).delete_all
    @meta.delete

    get root_url

    assert_redirected_to issues_url(org_slug: @acme.slug)
  end

  test "another Organization's page is still not found, and is not remembered" do
    other = create(:organization, name: "Elsewhere")
    get projects_url(org_slug: @meta.slug)

    get projects_url(org_slug: other.slug)
    assert_response :not_found
    get root_url

    assert_redirected_to issues_url(org_slug: @meta.slug)
  end
end
