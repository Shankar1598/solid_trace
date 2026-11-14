# frozen_string_literal: true

# OAuth for the MCP server (.scratch spec; ADR to come): OAuth clients,
# Grants, codes and OAuth tokens, in tables named oauth_*.
module Oauth
  def self.table_name_prefix
    "oauth_"
  end
end
