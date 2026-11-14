# frozen_string_literal: true

module Oauth
  # The registry of Scopes: each has a name, the resource it is for (a path
  # under the issuer) and the text that the approval page and the API
  # tokens page show, with %{organization} for the Organization's name. The
  # token check, the metadata, the 401, the approval page and the API tokens
  # page read it.
  #
  # Two flags say who can give a Scope:
  # - oauth: an OAuth client can ask for it on the approval page. Only the
  #   Scopes that a sign-in needs are: mcp and mcp:read for an MCP client,
  #   sessions:write and factory for `solidtrace login`. A Grant has no
  #   limits, so sessions:read (always limited) is never for OAuth.
  # - person: a person can put it on their own API token. tokens:create is
  #   only for the Dispatcher's token.
  module Scopes
    Scope = Data.define(:name, :resource_path, :text, :oauth, :person) do
      # The resource this Scope is for, under issuer (an Issuer).
      def resource(issuer)
        "#{issuer.url}#{resource_path}" if issuer.url
      end

      def oauth?
        oauth
      end

      def person?
        person
      end
    end

    # The resource of the factory's APIs: the agent ingest API and the
    # Dispatcher. They check a token through Console's tables or through
    # introspection, not at a Console path.
    FACTORY_PATH = "/factory"

    # The Scope that an API token with no Scope has, and that the 401 from
    # the MCP server asks for.
    MCP = "mcp"
    # The Scope that gives only the MCP server's read tools.
    MCP_READ = "mcp:read"
    # The Scopes that the MCP server accepts.
    FOR_MCP = [ MCP, MCP_READ ].freeze

    ALL = [
      Scope.new(
        name: MCP,
        resource_path: Issuer::MCP_PATH,
        text: "Use the MCP server as you, in %{organization}: read its Logs and Issues, and change its Dashboards, " \
              "Monitors, Issues and Projects as your role allows.",
        oauth: true, person: true,
      ),
      Scope.new(
        name: MCP_READ,
        resource_path: Issuer::MCP_PATH,
        text: "Read %{organization}'s Logs, Issues, Dashboards and Monitors through the MCP server, as you. " \
              "It cannot change them.",
        oauth: true, person: true,
      ),
      Scope.new(
        name: "sessions:write",
        resource_path: FACTORY_PATH,
        text: "Upload Agent sessions to %{organization}'s Agent Project, and read which parts of them it has.",
        oauth: true, person: true,
      ),
      Scope.new(
        name: "sessions:read",
        resource_path: FACTORY_PATH,
        text: "Download the Agent sessions that the token names from %{organization}'s Agent Project.",
        oauth: false, person: true,
      ),
      Scope.new(
        name: "run_events:write",
        resource_path: FACTORY_PATH,
        text: "Send run events to %{organization}'s Agent Project. It cannot start a Run.",
        oauth: false, person: true,
      ),
      Scope.new(
        name: "factory",
        resource_path: FACTORY_PATH,
        text: "Use the Dispatcher as you, in %{organization}: start, message and stop Runs, and do the Admin " \
              "actions that your role allows.",
        oauth: true, person: true,
      ),
      Scope.new(
        name: "issues:comment",
        resource_path: FACTORY_PATH,
        text: "Add comments to %{organization}'s Issues, as you.",
        oauth: false, person: true,
      ),
      Scope.new(
        name: "tokens:create",
        resource_path: FACTORY_PATH,
        text: "Make child tokens in %{organization} with some of this token's Scopes, limits that are as narrow " \
              "or narrower, and an expiry that is no later.",
        oauth: false, person: false,
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

    # The Scopes that an OAuth client can ask for.
    def for_oauth
      ALL.select(&:oauth?)
    end

    # The Scopes that a person can put on their own API token.
    def for_people
      ALL.select(&:person?)
    end

    # The Scopes that a request's space-separated scope asks for (mcp when it
    # names none), or nil when one is unknown or is not for OAuth.
    def requested(scope)
      names = scope.to_s.split
      names = [ MCP ] if names.empty?
      found = names.uniq.map { |name| find(name)&.then { |s| s if s.oauth? } }
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

    # The names of the Scopes for the resource at path under the issuer
    # that an OAuth client can ask for.
    def names_for(resource_path)
      for_oauth.select { |scope| scope.resource_path == resource_path }.map(&:name)
    end
  end
end
