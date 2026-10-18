# frozen_string_literal: true

require "test_helper"

class ApiTokensControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  setup do
    @organization = create(:organization)
    @user = create(:user)
    @organization.users << @user
    @other = create(:user)
    @others_token, = ApiToken.issue!(user: @other, name: "Theirs")

    sign_in_as(@user)
  end

  test "User settings list only the user's own tokens, by name, creation and last use" do
    mine, = ApiToken.issue!(user: @user, name: "Laptop")
    mine.touch_last_used

    props = inertia_props(user_settings_url)

    assert_equal [ "Laptop" ], props["api_tokens"].map { |t| t["name"] }
    listed = props["api_tokens"].first
    assert_equal({ "id" => mine.id, "name" => "Laptop", "created_at" => mine.created_at.iso8601,
                   "last_used_at" => mine.reload.last_used_at.iso8601 }, listed)
  end

  test "the Organization's User settings list them too, with the MCP URL as the request sees it" do
    ApiToken.issue!(user: @user, name: "Laptop")

    props = inertia_props(user_url(org_slug: @organization.slug))

    assert_equal [ "Laptop" ], props["api_tokens"].map { |t| t["name"] }
    assert_equal "http://www.example.com/mcp", props["mcp_url"]
  end

  test "making a token answers its secret once and keeps only the digest" do
    assert_difference -> { @user.api_tokens.count }, 1 do
      post api_tokens_url, params: { api_token: { name: "Laptop" } }, as: :json
    end

    assert_response :created
    secret = response.parsed_body["secret"]
    token = @user.api_tokens.last
    assert_match(/\Ast_/, secret)
    assert_equal token, ApiToken.find_by_secret(secret)
    assert_equal({ "id" => token.id, "name" => "Laptop", "created_at" => token.created_at.iso8601, "last_used_at" => nil },
      response.parsed_body["api_token"])

    get user_settings_url, headers: inertia_headers
    assert_not_includes response.body, secret
    assert_not_includes response.body, token.token_digest
  end

  test "a token needs a name" do
    assert_no_difference -> { ApiToken.count } do
      post api_tokens_url, params: { api_token: { name: "" } }, as: :json
    end

    assert_response :unprocessable_content
    assert_includes response.parsed_body.dig("errors", "name"), "can't be blank"
  end

  test "revoking a token deletes its row" do
    mine, secret = ApiToken.issue!(user: @user, name: "Laptop")

    delete api_token_url(mine)

    assert_redirected_to user_settings_url
    assert_nil ApiToken.find_by_secret(secret)
  end

  test "another user's token can't be revoked" do
    delete api_token_url(@others_token)

    assert_response :not_found
    assert ApiToken.exists?(@others_token.id)
  end

  test "signed out, no token is made" do
    delete session_url

    assert_no_difference -> { ApiToken.count } do
      post api_tokens_url, params: { api_token: { name: "Laptop" } }, as: :json
    end
  end

  private

  def inertia_headers
    { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
  end

  def inertia_props(url)
    get url, headers: inertia_headers
    assert_response :success
    assert_equal "Settings/User", response.parsed_body["component"]
    response.parsed_body["props"]
  end
end
