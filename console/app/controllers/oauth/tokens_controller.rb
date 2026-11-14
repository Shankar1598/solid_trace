# frozen_string_literal: true

module Oauth
  # The token endpoint. The authorization_code grant exchanges a code from
  # the approval page for an access token (sto_, 1 hour) and a refresh token
  # (str_). The request must give the code's client_id, the same
  # redirect_uri, the code's resource or none, and the code_verifier whose
  # S256 is the code's code_challenge; any difference is invalid_grant.
  class TokensController < ApiController
    def create
      case params[:grant_type]
      when "authorization_code" then exchange_code
      else
        oauth_error("unsupported_grant_type", "Send grant_type=authorization_code with the code from the approval page.")
      end
    end

    private

    def exchange_code
      client = Client.find_by(client_id: params[:client_id].to_s)
      return oauth_error("invalid_client", "Send the client_id that the client registered with.", status: :unauthorized) unless client

      code = Code.find_by_secret(params[:code])
      return invalid_grant unless code && code.kind == "authorization" && code.oauth_client_id == client.id

      # A code used a second time revokes the Grant it made (OAuth 2.1 §4.1.3).
      return invalid_grant(revoke: code.grant) if code.used_at
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
        verifier.is_a?(String) && code.code_challenge.present? &&
        ActiveSupport::SecurityUtils.secure_compare(Secret.s256(verifier), code.code_challenge)
    end

    def invalid_grant(revoke: nil)
      revoke&.revoke!
      oauth_error("invalid_grant", "The code is wrong, expired or used, or the request does not match it. " \
                                   "Start the sign-in again.")
    end
  end
end
