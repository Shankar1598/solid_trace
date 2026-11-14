# frozen_string_literal: true

require "test_helper"

class Oauth::IssuerTest < ActiveSupport::TestCase
  test "resources are equal after the scheme and host are lowercase and a trailing slash is removed" do
    assert Oauth::Issuer.same_resource?("https://console.example.com/mcp", "HTTPS://Console.Example.COM/mcp/")
    assert Oauth::Issuer.same_resource?("http://localhost:4001/mcp", "http://LOCALHOST:4001/mcp")
    assert Oauth::Issuer.same_resource?("https://console.example.com", "https://console.example.com/")
  end

  test "resources that differ in path case, path, port, scheme or host are not equal, and a blank one is never" do
    assert_not Oauth::Issuer.same_resource?("https://console.example.com/mcp", "https://console.example.com/MCP")
    assert_not Oauth::Issuer.same_resource?("https://console.example.com/mcp", "https://console.example.com/mcp/x")
    assert_not Oauth::Issuer.same_resource?("https://console.example.com/mcp", "https://console.example.com:8443/mcp")
    assert_not Oauth::Issuer.same_resource?("https://console.example.com/mcp", "http://console.example.com/mcp")
    assert_not Oauth::Issuer.same_resource?("https://console.example.com/mcp", "https://console.example.org/mcp")
    assert_not Oauth::Issuer.same_resource?(nil, nil)
    assert_not Oauth::Issuer.same_resource?("", "")
    assert_not Oauth::Issuer.same_resource?("mcp", "mcp")
    assert_not Oauth::Issuer.same_resource?("not a url", "not a url")
  end
end
