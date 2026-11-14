# frozen_string_literal: true

module Oauth
  # The token endpoint. The authorization_code grant exchanges a code from
  # the approval page for an access token (sto_, 1 hour) and a refresh token
  # (str_). The request must give the code's client_id, the same
  # redirect_uri, the code's resource or none, and the code_verifier whose
  # S256 is the code's code_challenge; any difference is invalid_grant.
  class TokensController < ApiController
    TOO_MANY = "Too many token requests. Try again in a minute."
    # 60 a minute for each IP address and for each client_id.
    rate_limit to: 60, within: 1.minute, only: :create, name: "ip",
               with: -> { oauth_error("invalid_request", TOO_MANY, status: :too_many_requests) }
    rate_limit to: 60, within: 1.minute, only: :create, name: "client",
               by: -> { asking_client_id.then { |id| id ? "client:#{id}" : "ip:#{request.remote_ip}" } },
               with: -> { oauth_error("invalid_request", TOO_MANY, status: :too_many_requests) }

    # A code_verifier: 43 to 128 unreserved characters (RFC 7636 §4.1).
    VERIFIER = /\A[A-Za-z0-9\-._~]{43,128}\z/

    def create
      case params[:grant_type]
      when "authorization_code" then exchange_code
      when nil, "" then oauth_error("invalid_request", "Send a grant_type: authorization_code with the code from the approval page.")
      else
        oauth_error("unsupported_grant_type", "Send grant_type=authorization_code with the code from the approval page.")
      end
    end

    private

    def exchange_code
      client = authenticated_client
      return unless client

      code = Code.find_by_secret(params[:code])
      return invalid_grant unless code && code.kind == "authorization" && code.oauth_client_id == client.id

      # A code used a second time revokes the Grant it made (OAuth 2.1 §4.1.3).
      return invalid_grant(revoke: code.grant) if code.used_at
      return invalid_target unless resource_in_grant?(code.grant)
      return invalid_grant unless code_matches?(code)
      return invalid_grant unless code.use!
      return invalid_grant unless code.grant&.live?(issuer: issuer)

      tokens = Token.issue_pair!(grant: code.grant, resource: code.resource)
      render json: {
        access_token: tokens[:access_token],
        token_type: "Bearer",
        expires_in: tokens[:expires_in],
        refresh_token: tokens[:refresh_token],
        scope: code.scopes.join(" "),
      }
    end

    def code_matches?(code)
      verifier = params[:code_verifier]
      !code.expired? &&
        params[:redirect_uri].to_s == code.redirect_uri &&
        (params[:resource].blank? || Issuer.same_resource?(params[:resource], code.resource)) &&
        verifier.is_a?(String) && VERIFIER.match?(verifier) && code.code_challenge.present? &&
        ActiveSupport::SecurityUtils.secure_compare(Secret.s256(verifier), code.code_challenge)
    end

    # Whether the request names no resource, or one of the Grant's
    # (RFC 8707). That the resource is the code's is checked with the code.
    def resource_in_grant?(grant)
      params[:resource].blank? || Array(grant&.resources).any? { |resource| Issuer.same_resource?(params[:resource], resource) }
    end

    def invalid_target
      oauth_error("invalid_target", "Send resource=#{issuer.mcp_resource}, the resource that the sign-in was for, or no resource.")
    end

    def invalid_grant(revoke: nil)
      revoke&.revoke!
      oauth_error("invalid_grant", "The code is wrong, expired or used, or the request does not match it. " \
                                   "Start the sign-in again.")
    end
  end
end
