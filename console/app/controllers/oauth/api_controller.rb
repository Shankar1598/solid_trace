# frozen_string_literal: true

module Oauth
  # What the OAuth JSON endpoints (token, registration) share: no Session
  # and no CSRF, as McpController; any origin may call them, as they read
  # no cookie; answers are never cached; and with OAuth off every one
  # answers 400 invalid_request with what to do instead.
  class ApiController < ActionController::API
    before_action :allow_any_origin
    before_action :require_oauth_on, except: :preflight

    # A browser's CORS preflight for a POST with a JSON body.
    def preflight
      response.set_header("Access-Control-Allow-Methods", "POST")
      response.set_header("Access-Control-Allow-Headers", "Authorization, Content-Type")
      response.set_header("Access-Control-Max-Age", "86400")
      head :no_content
    end

    private

    def issuer
      @issuer ||= Issuer.current
    end

    def require_oauth_on
      oauth_error("invalid_request", Issuer::OFF_DESCRIPTION) unless issuer.on?
    end

    def allow_any_origin
      response.set_header("Access-Control-Allow-Origin", "*")
      response.set_header("Cache-Control", "no-store")
    end

    # An OAuth error answer (OAuth 2.1 §3.2.4): error, and a description that
    # says what to do.
    def oauth_error(error, description, status: :bad_request)
      render json: { error: error, error_description: description }, status: status
    end
  end
end
