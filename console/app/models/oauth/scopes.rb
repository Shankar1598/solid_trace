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
      )
    ].freeze

    module_function

    def names
      ALL.map(&:name)
    end

    # The Scope with this name, or nil.
    def find(name)
      ALL.find { |scope| scope.name == name }
    end

    # The Scopes that a request's space-separated scope asks for (mcp when it
    # names none), or nil when one is unknown.
    def requested(scope)
      names = scope.to_s.split
      names = [ "mcp" ] if names.empty?
      found = names.uniq.map { |name| find(name) }
      found unless found.include?(nil)
    end

    # The resource that a code for scopes is for: the request's resource,
    # when one of the Scopes is for it, else the Scopes' own; nil when no
    # Scope is for it (RFC 8707).
    def resource_for(scopes, resource, issuer:)
      resources = scopes.map { |scope| scope.resource(issuer) }.uniq
      return resources.first if resource.blank?

      resources.find { |candidate| Issuer.same_resource?(candidate, resource) }
    end

    # The names of the Scopes for the resource at path under the issuer.
    def names_for(resource_path)
      ALL.select { |scope| scope.resource_path == resource_path }.map(&:name)
    end
  end
end
