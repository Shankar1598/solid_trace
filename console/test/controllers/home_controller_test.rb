# frozen_string_literal: true

require "test_helper"

# `/`, where signing in lands, opens the Overview of the Organization the user
# last opened, while they are still in it, and otherwise of their first
# Organization by name.
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

    assert_redirected_to overview_url(org_slug: @meta.slug)
  end

  test "any page of an Organization counts as opening it" do
    get organization_url(org_slug: @meta.slug)
    get user_url(org_slug: @meta.slug)

    get root_url

    assert_redirected_to overview_url(org_slug: @meta.slug)
  end

  test "a JSON request under an Organization doesn't make it the last used" do
    get projects_url(org_slug: @acme.slug)

    get mentions_url(org_slug: @meta.slug, format: :json)
    assert_response :success
    get root_url

    assert_redirected_to overview_url(org_slug: @acme.slug)
  end

  test "home opens the first Organization by name before any is opened" do
    zeta = create(:organization, name: "Zeta")
    create(:organization_user, organization: zeta, user: @user)

    get root_url

    assert_redirected_to overview_url(org_slug: @acme.slug)
  end

  test "a user removed from the last used Organization is sent to their first by name" do
    get projects_url(org_slug: @meta.slug)
    @meta.organization_users.find_by!(user: @user).discard

    get root_url

    assert_redirected_to overview_url(org_slug: @acme.slug)
  end

  test "a user whose last used Organization was deleted is sent to their first by name" do
    get projects_url(org_slug: @meta.slug)
    OrganizationUser.where(organization: @meta).delete_all
    @meta.delete

    get root_url

    assert_redirected_to overview_url(org_slug: @acme.slug)
  end

  test "every Organization page shares the user's Organizations by name, in any case, and only theirs" do
    create(:organization, name: "Elsewhere")
    beta = create(:organization, name: "beta")
    create(:organization_user, organization: beta, user: @user)
    @meta.organization_users.create!(user: create(:user))

    get projects_url(org_slug: @meta.slug), headers: inertia

    assert_equal [ { "name" => "Acme", "slug" => @acme.slug }, { "name" => "beta", "slug" => beta.slug },
                  { "name" => "Meta", "slug" => @meta.slug } ],
      response.parsed_body.dig("props", "organizations")
  end

  test "another Organization's page is still not found, and is not remembered" do
    other = create(:organization, name: "Elsewhere")
    get projects_url(org_slug: @meta.slug)

    get projects_url(org_slug: other.slug)
    assert_response :not_found
    get root_url

    assert_redirected_to overview_url(org_slug: @meta.slug)
  end

  private

  def inertia
    { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
  end
end
