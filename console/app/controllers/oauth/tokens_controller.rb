# frozen_string_literal: true

module Oauth
  # The token endpoint. The authorization_code grant exchanges a code from
  # the approval page for an access token (sto_, 1 hour) and a refresh token
  # (str_). The request must give the code's client_id, the same
  # redirect_uri, the code's resource or none, and the code_verifier whose
  # S256 is the code's code_challenge; any difference is invalid_grant.
  #
  # The refresh_token grant gives a new pair for a refresh token, which
  # works once (Token#rotate!). Its Grant must be live and the Grant's
  # client must ask. A used refresh token in the 60 seconds after its first
  # use gets the same pair again; after that, it revokes the Grant.
  #
  # The device code grant (RFC 8628 §3.4) is the program's poll: it answers
  # authorization_pending until the user approves on the device page,
  # slow_down (and 5 seconds more on the interval) when the program polls
  # faster than its interval, access_denied after Deny, expired_token after
  # 10 minutes, and the tokens once, with one conditional update.
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
      when "refresh_token" then refresh
      when DeviceCode::GRANT_TYPE then poll_device_code
      when nil, "" then oauth_error("invalid_request", "Send a grant_type: authorization_code with the code from the " \
                                                       "approval page, or refresh_token.")
      else
        oauth_error("unsupported_grant_type", "Send grant_type=authorization_code with the code from the approval " \
                                              "page, or grant_type=refresh_token with a refresh token.")
      end
    end

    private

    def refresh
      client = authenticated_client
      return unless client

      token = Token.find_refresh(params[:refresh_token])
      grant = token&.grant
      return invalid_refresh unless grant && grant.oauth_client_id == client.id
      unless client.grant_types.include?("refresh_token")
        return oauth_error("unauthorized_client", "This client did not register the refresh_token grant. Sign in again.")
      end
      return invalid_refresh unless grant.live?(issuer: issuer) && token.revoked_at.nil?
      return invalid_target unless resource_in_grant?(grant)
      return invalid_scope unless scope_in_grant?(grant)
      return invalid_refresh if token.used_at.nil? && token.expired?

      tokens = token.rotate!(resource: grant_resource(grant))
      # A used refresh token after the window: a copy is in other hands.
      return invalid_refresh(revoke: grant) unless tokens

      grant.touch_last_used
      render json: {
        access_token: tokens[:access_token],
        token_type: "Bearer",
        expires_in: tokens[:expires_in],
        refresh_token: tokens[:refresh_token],
        scope: grant.scopes.join(" "),
      }
    end

    # The Grant's resource that the request names, else the Grant's first.
    def grant_resource(grant)
      return grant.resources.first if params[:resource].blank?

      grant.resources.find { |resource| Issuer.same_resource?(params[:resource], resource) }
    end

    # Whether the request names no scope, or only Scopes of the Grant. The
    # new pair always has every Scope of the Grant.
    def scope_in_grant?(grant)
      params[:scope].blank? || (params[:scope].is_a?(String) && (params[:scope].split - grant.scopes).empty?)
    end

    def invalid_scope
      oauth_error("invalid_scope", "Send no scope, or only the Scopes of the sign-in.")
    end

    def invalid_refresh(revoke: nil)
      revoke&.revoke!
      oauth_error("invalid_grant", "The refresh token is wrong, expired, used or revoked, or is for another client. " \
                                   "Sign in again.")
    end

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

      render_tokens(code)
    end

    # A client with a published description is public: it sends its
    # client_id in the body, and no secret. Its row may not exist before a
    # user types the code, so the client_id is compared with the device
    # code's, not looked up. Each answer to a poll is a 400 with its error
    # (RFC 8628 §3.5).
    def poll_device_code
      client_id = params[:client_id]
      return invalid_client unless client_id.is_a?(String) && client_id.present?
      return invalid_client if basic_credentials || params.key?(:client_secret)

      code = Code.find_by_secret(params[:device_code])
      return invalid_device_code unless code && code.kind == "device" && code.client_id == client_id && code.used_at.nil?
      return oauth_error("access_denied", "The user denied the sign-in. Ask for a new code to try again.") if code.denied_at
      return oauth_error("expired_token", "The device code is older than 10 minutes. Ask for a new one.") if code.expired?
      return oauth_error("slow_down", "Poll at most once in #{code.reload.poll_interval} seconds.") unless code.poll!
      unless code.oauth_grant_id
        return oauth_error("authorization_pending", "The user has not approved yet. Poll again in " \
                                                    "#{code.poll_interval} seconds.")
      end
      return invalid_device_code unless code.use! && code.grant.live?(issuer: issuer)

      render_tokens(code)
    end

    # An access token for the code's resource and a refresh token, under
    # the code's Grant.
    def render_tokens(code)
      tokens = Token.issue_pair!(grant: code.grant, resource: code.resource)
      render json: {
        access_token: tokens[:access_token],
        token_type: "Bearer",
        expires_in: tokens[:expires_in],
        refresh_token: tokens[:refresh_token],
        scope: code.scopes.join(" "),
      }
    end

    def invalid_device_code
      oauth_error("invalid_grant", "The device code is wrong or used. Ask for a new one.")
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
