# frozen_string_literal: true

require "test_helper"

# The decisions on a device code that the HTTP tests cannot make certain:
# two decisions read one waiting device code, and both try to decide it.
# Only the first counts.
class Oauth::CodeTest < ActiveSupport::TestCase
  RESOURCE = "https://console.example.com/mcp"

  setup do
    user = create(:user)
    organization = create(:organization)
    create(:organization_user, organization: organization, user: user)
    client = Oauth::Client.create!(kind: "cimd", client_id: "https://cli.example.com/oauth/client.json", name: "CLI",
                                   redirect_uris: [], grant_types: [ Oauth::DeviceCode::GRANT_TYPE ])
    @grant = Oauth::Grant.create!(user: user, client: client, organization: organization, scopes: [ "mcp" ],
                                  resources: [ RESOURCE ], approved_via: "device", approved_at: Time.current)
    @code, = Oauth::DeviceCode.issue!(client_id: client.client_id, scopes: Oauth::Scopes.requested("mcp"),
                                      resource: RESOURCE, ip_address: "203.0.113.7")
  end

  test "Approve after Deny does not count, and sets no Grant" do
    other = Oauth::Code.find(@code.id)

    assert @code.deny!
    assert_not other.approve!(@grant)

    assert_nil @code.reload.oauth_grant_id
    assert @code.denied_at
  end

  test "Deny after Approve does not count, and keeps the Grant" do
    other = Oauth::Code.find(@code.id)

    assert @code.approve!(@grant)
    assert_not other.deny!

    assert_equal @grant.id, @code.reload.oauth_grant_id
    assert_nil @code.denied_at
  end

  test "no decision counts on a used or expired device code" do
    assert @code.use!
    assert_not @code.approve!(@grant)
    assert_not @code.deny!

    expired, = Oauth::DeviceCode.issue!(client_id: @code.client_id, scopes: Oauth::Scopes.requested("mcp"),
                                        resource: RESOURCE, ip_address: "203.0.113.7")
    travel Oauth::DeviceCode::LASTS + 1.second
    assert_not expired.approve!(@grant)
    assert_not expired.deny!
  end
end
