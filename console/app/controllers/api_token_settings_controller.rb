# frozen_string_literal: true

# Settings → API tokens & MCP: the user's API tokens, which agents sign in to
# the MCP server with, and the URL to add the server at. Tokens are made and
# revoked by ApiTokensController.
class ApiTokenSettingsController < ApplicationController
  layout "dashboard"
  before_action :set_current_org

  def show
    render inertia: "Settings/ApiTokens", props: {
      api_tokens: current_user.api_tokens.order(created_at: :desc, id: :desc).map { |t| ApiTokenSerializer.new(t).as_json },
      # Where a client adds the MCP server, as this request reaches Console.
      mcp_url: "#{request.base_url}/mcp",
    }
  end
end
