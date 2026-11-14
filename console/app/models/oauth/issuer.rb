# frozen_string_literal: true

module Oauth
  # The OAuth issuer: the Console URL setting, else CONSOLE_URL, with no
  # trailing slash (PublicUrls with no request). It never comes from the
  # address a request arrived on, so the metadata and the tokens always name
  # the same server, whatever host a request came on.
  #
  # OAuth is on only when the issuer is https://, or http:// at a loopback
  # host (localhost, 127.0.0.1, [::1]) for development and tests, and is an
  # origin with no path, as Console runs at the root of its host. Elsewhere
  # it is off, and API tokens work alone.
  class Issuer
    LOOPBACK_HOSTS = %w[localhost 127.0.0.1 [::1]].freeze
    # Where the MCP server is under the issuer, and so its resource.
    MCP_PATH = "/mcp"
    # Where the MCP server's protected resource metadata is (RFC 9728 §3.1).
    RESOURCE_METADATA_PATH = "/.well-known/oauth-protected-resource#{MCP_PATH}".freeze
    # What the OAuth endpoints and the approval page say with OAuth off.
    OFF_DESCRIPTION = "Sign-in through the browser needs an HTTPS Console URL. Ask the operator to set one, " \
                      "or use an API token from Settings → API tokens & MCP."

    attr_reader :url

    def self.current(settings: InstanceSetting.current, env: ENV)
      new(PublicUrls.console_url(nil, settings: settings, env: env))
    end

    # Whether two resources are the same: equal once the scheme and the host
    # are lowercase and a trailing slash is removed. A blank one is never
    # equal to another.
    def self.same_resource?(one, other)
      a = normalize_resource(one)
      b = normalize_resource(other)
      !a.nil? && a == b
    end

    def self.normalize_resource(resource)
      uri = URI(resource.to_s.strip)
      return if uri.scheme.nil? || uri.host.nil?

      uri.scheme = uri.scheme.downcase
      uri.host = uri.host.downcase
      uri.to_s.sub(%r{/+\z}, "")
    rescue URI::InvalidURIError
      nil
    end

    def initialize(url)
      @url = PublicUrls.clean(url)
      @on = oauth_allowed?
    end

    # Whether OAuth sign-in works on this install.
    def on?
      @on
    end

    # The MCP server's resource, <issuer>/mcp; nil with no Console URL.
    def mcp_resource
      "#{url}#{MCP_PATH}" if url
    end

    # Where a client reads the MCP server's protected resource metadata.
    def resource_metadata_url
      "#{url}#{RESOURCE_METADATA_PATH}" if url
    end

    private

    def oauth_allowed?
      return false if url.nil?

      uri = URI(url)
      return false unless uri.host.present? && uri.path.empty? && uri.userinfo.nil? && uri.query.nil? && uri.fragment.nil?

      case uri.scheme&.downcase
      when "https" then true
      when "http" then LOOPBACK_HOSTS.include?(uri.host.downcase)
      else false
      end
    rescue URI::InvalidURIError
      false
    end
  end
end
