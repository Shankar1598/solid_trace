# frozen_string_literal: true

module Oauth
  # The registry of Scopes: each has a name, the resource it is for (a path
  # under the issuer) and the text that the approval page shows, with
  # %{organization} for the Organization's name. The token check, the
  # metadata and the 401 read it. Later efforts add their Scopes here.
  module Scopes
    Scope = Data.define(:name, :resource_path, :text) do
      # The resource this Scope is for, under issuer (an Issuer).
      def resource(issuer)
        "#{issuer.url}#{resource_path}" if issuer.url
      end
    end

    ALL = [
      Scope.new(
        name: "mcp",
        resource_path: Issuer::MCP_PATH,
        text: "Use the MCP server as you, in %{organization}: read its Logs and Issues, and change its Dashboards, " \
              "Monitors, Issues and Projects as your role allows.",
      ),
    ].freeze

    module_function

    def names
      ALL.map(&:name)
    end

    # The Scope with this name, or nil.
    def find(name)
      ALL.find { |scope| scope.name == name }
    end

    # The names of the Scopes for the resource at path under the issuer.
    def names_for(resource_path)
      ALL.select { |scope| scope.resource_path == resource_path }.map(&:name)
    end
  end
end
