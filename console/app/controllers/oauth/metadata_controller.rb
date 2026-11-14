# frozen_string_literal: true

module Oauth
  # The two documents an MCP client reads to find how to sign in: the MCP
  # server's protected resource metadata (RFC 9728), which the 401 from
  # POST /mcp points to, and the authorization server metadata (RFC 8414).
  # Both name the issuer (Oauth::Issuer), never the request's host, and any
  # origin can read them (they read no cookie). With OAuth off, both are 404.
  class MetadataController < ActionController::API
    before_action :require_oauth
    after_action :allow_any_origin

    def protected_resource
      render json: {
        resource: @issuer.mcp_resource,
        authorization_servers: [ @issuer.url ],
        scopes_supported: Scopes.names_for(Issuer::MCP_PATH),
        bearer_methods_supported: [ "header" ],
        resource_name: "SolidTrace",
      }
    end

    # The endpoints are named here before each exists: a later ticket adds
    # each one at these paths.
    def authorization_server
      issuer = @issuer.url
      render json: {
        issuer: issuer,
        authorization_endpoint: "#{issuer}/oauth/authorize",
        token_endpoint: "#{issuer}/oauth/token",
        registration_endpoint: "#{issuer}/oauth/register",
        revocation_endpoint: "#{issuer}/oauth/revoke",
        introspection_endpoint: "#{issuer}/oauth/introspect",
        device_authorization_endpoint: "#{issuer}/oauth/device_authorization",
        response_types_supported: [ "code" ],
        grant_types_supported: [ "authorization_code", "refresh_token", "urn:ietf:params:oauth:grant-type:device_code" ],
        code_challenge_methods_supported: [ "S256" ],
        token_endpoint_auth_methods_supported: [ "none", "client_secret_post", "client_secret_basic" ],
        scopes_supported: Scopes.names,
        client_id_metadata_document_supported: true,
        authorization_response_iss_parameter_supported: true,
      }
    end

    private

    def require_oauth
      @issuer = Issuer.current
      head :not_found unless @issuer.on?
    end

    def allow_any_origin
      response.set_header("Access-Control-Allow-Origin", "*")
    end
  end
end
