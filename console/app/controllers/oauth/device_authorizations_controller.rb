# frozen_string_literal: true

module Oauth
  # Device authorization (RFC 8628 §3.1), for a program with no browser
  # whose client_id is the URL of its published description. It answers a
  # device code to poll the token endpoint with, the user code to type and
  # the device page's URL, and never a URL with the code filled in. It reads
  # only the kept copy of the description, and fetches nothing: the device
  # page checks the description once a signed-in user types the code.
  #
  # A registered (DCR) client, and a fresh kept copy that does not list the
  # device grant, get unauthorized_client. CIMD clients are public, so no
  # client authentication is needed.
  class DeviceAuthorizationsController < ApiController
    TOO_MANY = "Too many device code requests. Try again in a minute."
    # 60 a minute for each IP address and for each client_id.
    rate_limit to: 60, within: 1.minute, only: :create, name: "ip",
               with: -> { oauth_error("invalid_request", TOO_MANY, status: :too_many_requests) }
    rate_limit to: 60, within: 1.minute, only: :create, name: "client",
               by: -> { params[:client_id].is_a?(String) ? "client:#{params[:client_id]}" : "ip:#{request.remote_ip}" },
               with: -> { oauth_error("invalid_request", TOO_MANY, status: :too_many_requests) }

    NOT_PUBLISHED = "The device flow is only for a client whose client_id is the https:// URL of its published " \
                    "description, with #{DeviceCode::GRANT_TYPE} in its grant_types."

    def create
      client_id = params[:client_id]
      return unknown_client unless client_id.is_a?(String) && client_id.present?

      kept = Client.find_by(client_id: client_id)
      return oauth_error("unauthorized_client", NOT_PUBLISHED) if kept && !kept.name_checked?
      return unknown_client unless PublishedDescription.url?(client_id) && PublishedDescription.well_formed?(client_id)
      if kept&.fresh? && !kept.grant_types.include?(DeviceCode::GRANT_TYPE)
        return oauth_error("unauthorized_client", NOT_PUBLISHED)
      end

      scopes = Scopes.requested(params[:scope])
      unless scopes
        return oauth_error("invalid_scope", "Ask for the scopes in the metadata's scopes_supported: #{Scopes.names.join(', ')}.")
      end
      resource = Scopes.resource_for(scopes, params[:resource], issuer: issuer)
      return oauth_error("invalid_target", "Send resource=#{issuer.mcp_resource}, or no resource.") unless resource

      _code, device_code, user_code = DeviceCode.issue!(client_id: client_id, scopes: scopes, resource: resource,
                                                         ip_address: request.remote_ip)
      render json: {
        device_code: device_code,
        user_code: user_code,
        verification_uri: "#{issuer.url}/device",
        expires_in: DeviceCode::LASTS.to_i,
        interval: DeviceCode::INTERVAL,
      }
    end

    private

    def unknown_client
      oauth_error("invalid_client", NOT_PUBLISHED, status: :unauthorized)
    end
  end
end
