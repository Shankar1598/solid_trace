require "test_helper"

class MentionsControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  setup do
    @organization = create(:organization)
    @user = create(:user)
    @organization.users << @user
    sign_in_as @user
  end

  test "should get mentions list" do
    get mentions_url(org_slug: @organization.slug, format: :json)
    assert_response :success

    json_response = JSON.parse(response.body)
    assert_kind_of Array, json_response
    assert_equal 1, json_response.length
    assert_equal @user.id, json_response.first["id"]
  end
end
