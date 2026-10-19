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
end
