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
      api_tokens: current_user.api_tokens.includes(:organization, :project).order(created_at: :desc, id: :desc)
                              .map { |t| ApiTokenSerializer.new(t).as_json },
      # The Scopes a person can put on their own token, with the text that
      # says what each gives, and the Organizations a token can be for.
      scopes: Oauth::Scopes.for_people.map { |scope| { name: scope.name, text: scope.text } },
      organizations: current_user.organizations_by_name.map { |o| { id: o.id, slug: o.slug, name: o.name } },
      # The Projects of each Organization, for the mcp:read Project limit.
      projects: current_user.organizations_by_name.includes(:projects).flat_map do |organization|
        organization.projects.sort_by(&:name).map { |p| { id: p.id, slug: p.slug, name: p.name, organization_id: organization.id } }
      end,
      # Where a client adds the MCP server, at Console's URL.
      mcp_url: "#{console_url}/mcp",
      # Why MCP clients cannot sign in through the browser here; nil when
      # they can (Oauth::Issuer).
      browser_sign_in_note: (BROWSER_SIGN_IN_NOTE unless Oauth::Issuer.current.on?),
    }
  end
end
