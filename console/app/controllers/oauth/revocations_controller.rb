# frozen_string_literal: true

module Oauth
  # The revocation endpoint (RFC 7009), for a client's sign-out (as
  # `claude mcp logout`). An access token or a refresh token of the asking
  # client revokes its whole Grant. Every well-formed request answers 200,
  # also for an unknown token, another client's token or an API token
  # (st_), which only Settings revokes.
  class RevocationsController < ApiController
    TOO_MANY = "Too many revocation requests. Try again in a minute."
    # 60 a minute for each IP address and for each client_id (for
    # solidtrace-cli, for each IP address: see client_limit_key).
    rate_limit to: 60, within: 1.minute, only: :create, name: "ip",
               with: -> { oauth_error("invalid_request", TOO_MANY, status: :too_many_requests) }
    rate_limit to: 60, within: 1.minute, only: :create, name: "client",
               by: -> { client_limit_key(asking_client_id) },
               with: -> { oauth_error("invalid_request", TOO_MANY, status: :too_many_requests) }

    def create
      client = authenticated_client
      return unless client
      unless params[:token].is_a?(String) && params[:token].present?
        return oauth_error("invalid_request", "Send token: the access token or the refresh token to revoke.")
      end

      grant = Token.find_by_secret(params[:token])&.grant
      grant.revoke! if grant && grant.oauth_client_id == client.id
      head :ok
    end
  end
end
