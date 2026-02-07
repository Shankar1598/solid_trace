# frozen_string_literal: true

require "test_helper"

class OrganizationSettingsControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  setup do
    @organization = create(:organization)
    @user = create(:user)
    @organization.users << @user

    sign_in_as(@user)
  end

  test "show returns success" do
    get organization_path(org_slug: @organization.slug)

    assert_response :success
  end

  test "update organization name" do
    patch organization_path(org_slug: @organization.slug), params: {
      organization: { name: "New Org Name" }
    }

    assert_redirected_to organization_path(org_slug: @organization.slug)
    assert_equal "New Org Name", @organization.reload.name
  end

  test "update organization slug" do
    patch organization_path(org_slug: @organization.slug), params: {
      organization: { slug: "new-slug" }
    }

    assert_redirected_to organization_path(org_slug: "new-slug")
    assert_equal "new-slug", @organization.reload.slug
  end

  test "denies access to other org" do
    other_org = create(:organization)

    get organization_path(org_slug: other_org.slug)

    assert_response :not_found
  end
end
