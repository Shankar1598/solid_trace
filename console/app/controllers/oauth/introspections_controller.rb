# frozen_string_literal: true

module Oauth
  # Token introspection (RFC 7662): a service that holds an API token sends
  # a token that a user gave it, form-encoded as token=<secret>, and Console
  # answers who the token acts as. An OAuth access token and an API token
  # answer in one shape (BearerToken), and so does Console's factory token
  # (Oauth::ConsoleToken); every other token answers only active: false.
  # The caller authenticates with its own API token in Authorization:
  # Bearer. An OAuth access token cannot be the caller, as its audience is
  # the MCP server, and Console's factory token cannot, as it is only for
  # the Dispatcher. This endpoint answers also with OAuth off,
  # because it serves API tokens.
  class IntrospectionsController < ApiController
    CALLER = "Send your own API token as Authorization: Bearer st_…. Make one in Settings → API tokens & MCP. " \
             "An OAuth access token works only at the MCP server."
    TOO_MANY = "Too many introspection requests. Try again in a minute."

    skip_before_action :require_oauth_on
    # 120 a minute for each caller token.
    rate_limit to: 120, within: 1.minute, only: :create,
               by: -> { caller_secret ? "token:#{Secret.digest(caller_secret)}" : "ip:#{request.remote_ip}" },
               with: -> { oauth_error("invalid_request", TOO_MANY, status: :too_many_requests) }
    before_action :authenticate_caller!

    def create
      token = params[:token]
      unless token.is_a?(String) && token.present?
        return oauth_error("invalid_request", "Send the token to check as token=<secret>, form-encoded.")
      end

      bearer = BearerToken.authenticate(token, issuer: issuer)
      render json: bearer ? active(bearer) : { active: false }
    end

    # The caller's user, for the request's log line (RequestLog).
    def current_user
      @caller&.user
    end

    private

    # The secret in Authorization: Bearer, or nil.
    def caller_secret
      request.authorization.to_s[/\ABearer +(\S+)\z/i, 1]
    end

    def authenticate_caller!
      @caller = BearerToken.authenticate(caller_secret, issuer: issuer)
      @caller = nil unless @caller&.api_token?
      if @caller
        @caller.record_use
      else
        response.set_header("WWW-Authenticate", %(Bearer error="invalid_token"))
        oauth_error("invalid_token", CALLER, status: :unauthorized)
      end
    end

    # The answer for an active token. The roles are read now, so a change
    # of role shows at once.
    def active(bearer)
      {
        active: true,
        token_type: bearer.kind.to_s,
        sub: bearer.user.id.to_s,
        username: bearer.user.email,
        client_id: bearer.client_id,
        scope: bearer.scopes.join(" "),
        aud: bearer.resource,
        iat: bearer.issued_at.to_i,
        exp: bearer.expires_at&.to_i,
        organizations: bearer.memberships.map do |membership|
          organization = membership.organization
          { id: organization.id, slug: organization.slug, name: organization.name, role: membership.role }
        end,
        limits: bearer.limits,
      }.compact
    end
  end
end
