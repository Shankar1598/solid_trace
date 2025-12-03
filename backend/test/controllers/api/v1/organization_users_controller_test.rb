require "test_helper"

module Api
  module V1
    class OrganizationUsersControllerTest < ActionDispatch::IntegrationTest
      setup do
        @user = create(:user)
        @organization = create(:organization)
        @organization.users << @user
        @token = JWT.encode({ user_id: @user.id }, Rails.application.credentials.secret_key_base)
        @headers = { "Authorization" => "Bearer #{@token}" }
      end

      test "should invite user" do
        assert_emails 1 do
          post "/api/v1/#{@organization.slug}/invite", params: { email: "invitee@example.com" }, headers: @headers
        end
        assert_response :success
        assert_equal "Invitation sent", JSON.parse(response.body)["message"]
      end

      test "should return error if email is missing" do
        post "/api/v1/#{@organization.slug}/invite", params: { email: "" }, headers: @headers
        assert_response :unprocessable_entity
        assert_equal "Email is required", JSON.parse(response.body)["error"]
      end
    end
  end
end
