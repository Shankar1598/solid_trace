# frozen_string_literal: true

require "test_helper"

# The race that the HTTP tests cannot make certain: two requests read one
# unused refresh token, and both rotate it.
class Oauth::TokenTest < ActiveSupport::TestCase
  setup do
    user = create(:user)
    organization = create(:organization)
    create(:organization_user, organization: organization, user: user)
    client = Oauth::Client.create!(kind: "dcr", client_id: "client-1", name: "Claude Code",
                                   redirect_uris: [ "http://localhost:4567/callback" ],
                                   grant_types: Oauth::Client::DEFAULT_GRANT_TYPES)
    @grant = Oauth::Grant.create!(user: user, client: client, organization: organization, scopes: [ "mcp" ],
                                  resources: [ "https://console.example.com/mcp" ], approved_via: "browser",
                                  approved_at: Time.current)
    @refresh = Oauth::Token.issue_pair!(grant: @grant, resource: "https://console.example.com/mcp")[:refresh_token]
  end

  test "the request that loses the race gets the winner's pair, and its own pair is not kept" do
    winner = Oauth::Token.find_refresh(@refresh)
    loser = Oauth::Token.find_refresh(@refresh)

    won = winner.rotate!(resource: "https://console.example.com/mcp")
    lost = loser.rotate!(resource: "https://console.example.com/mcp")

    assert_equal won.slice(:access_token, :refresh_token), lost.slice(:access_token, :refresh_token)
    assert_equal 2, @grant.tokens.access.count
    assert_equal 1, @grant.tokens.refresh.where(used_at: nil).count
  end
end
