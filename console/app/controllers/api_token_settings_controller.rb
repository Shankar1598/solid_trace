# frozen_string_literal: true

# Settings → API tokens & MCP: the user's API tokens, which agents sign in to
# the MCP server with, and the URL to add the server at. Tokens are made and
# revoked by ApiTokensController.
class ApiTokenSettingsController < ApplicationController
  layout "dashboard"
  before_action :set_current_org

  # Settings → Instance cannot turn HTTPS on: its Console URL keeps the
  # scheme that CONSOLE_URL gave at the start (InstanceSetting).
  BROWSER_SIGN_IN_NOTE = "Sign-in through the browser needs an HTTPS Console URL, so MCP clients use API tokens here. " \
                         "To turn it on, the operator sets CONSOLE_URL to an https:// address in deploy/.env and " \
                         "restarts Console. If Settings → Instance has an http:// Console URL, an Instance admin then " \
                         "changes it to https:// or clears it."

  def show
    render inertia: "Settings/ApiTokens", props: {
      api_tokens: current_user.api_tokens.order(created_at: :desc, id: :desc).map { |t| ApiTokenSerializer.new(t).as_json },
      # Where a client adds the MCP server, at Console's URL.
      mcp_url: "#{console_url}/mcp",
      # Why MCP clients cannot sign in through the browser here; nil when
      # they can (Oauth::Issuer).
      browser_sign_in_note: (BROWSER_SIGN_IN_NOTE unless Oauth::Issuer.current.on?),
    }
  end
end
