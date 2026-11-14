# frozen_string_literal: true

module Oauth
  # Dynamic Client Registration (RFC 7591), open with no initial token, for
  # clients that do not publish a description. A client registers as a
  # public client (token_endpoint_auth_method none) with HTTPS or loopback
  # redirect URIs. Its name is its own claim: the approval page says Console
  # did not check it, and a missing one is "Unnamed client".
  class RegistrationsController < ApiController
    rate_limit to: 10, within: 3.minutes, only: :create, with: -> {
      oauth_error("invalid_request", "Too many registrations from this address. Try again in a few minutes.",
                  status: :too_many_requests)
    }

    def create
      body = request.request_parameters
      redirect_uris = body["redirect_uris"]
      grant_types = body["grant_types"].presence || Client::DEFAULT_GRANT_TYPES
      auth_method = body["token_endpoint_auth_method"].presence || "none"
      name = body["client_name"].presence || Client::UNNAMED

      unless redirect_uris.is_a?(Array) && redirect_uris.any? && redirect_uris.all? { |uri| Client.allowed_redirect_uri?(uri) }
        return oauth_error("invalid_redirect_uri", "Give redirect_uris: one or more https:// URIs, or http:// URIs at " \
                                                   "localhost, 127.0.0.1 or [::1], each at most 512 characters.")
      end
      unless grant_types.is_a?(Array) && grant_types.all? { |type| Client::DEFAULT_GRANT_TYPES.include?(type) }
        return oauth_error("invalid_client_metadata", "Give grant_types from authorization_code and refresh_token. " \
                                                      "The device grant needs a client with a published description.")
      end
      unless auth_method == "none"
        return oauth_error("invalid_client_metadata", "Register as a public client: token_endpoint_auth_method none, " \
                                                      "with PKCE.")
      end
      unless name.is_a?(String) && name.length <= 100
        return oauth_error("invalid_client_metadata", "Give a client_name of at most 100 characters.")
      end

      client = Client.create!(kind: "dcr", client_id: Secret.generate, name: name, redirect_uris: redirect_uris,
                              grant_types: grant_types, token_endpoint_auth_method: auth_method)
      render status: :created, json: {
        client_id: client.client_id,
        client_id_issued_at: client.created_at.to_i,
        client_name: client.name,
        redirect_uris: client.redirect_uris,
        grant_types: client.grant_types,
        response_types: [ "code" ],
        token_endpoint_auth_method: client.token_endpoint_auth_method,
      }
    end
  end
end
