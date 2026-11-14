# frozen_string_literal: true

module Oauth
  # What the OAuth JSON endpoints (token, registration) share: no Session
  # and no CSRF, as McpController; any origin may call them, as they read
  # no cookie; answers are never cached; with OAuth off every one answers
  # 400 invalid_request with what to do instead; a body that cannot be read
  # is invalid_request; and client authentication (#authenticated_client).
  class ApiController < ActionController::API
    INVALID_CLIENT = "Send the client_id that the client registered with, and for a confidential client its " \
                     "client_secret, in Authorization: Basic or in the body, not both."

    before_action :allow_any_origin
    before_action :require_oauth_on, except: :preflight

    rescue_from ActionDispatch::Http::Parameters::ParseError do
      oauth_error("invalid_request", "Console could not read the request body. Send JSON to /oauth/register, and a " \
                                     "form (application/x-www-form-urlencoded) to the other endpoints.")
    end

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

    # The client that asks, authenticated: a confidential client by
    # client_secret_basic or client_secret_post, a public client by its
    # client_id alone (with no secret). Else nil, after the error answer.
    def authenticated_client
      basic = basic_credentials
      return invalid_client if basic == :malformed

      if basic
        if params.key?(:client_secret)
          return oauth_error("invalid_request", "Send the client secret one way only: Authorization: Basic or " \
                                                "client_secret in the body.") && nil
        end
        return invalid_client if params[:client_id].present? && params[:client_id] != basic[0]

        client_id, secret = basic
      else
        client_id, secret = params[:client_id], params[:client_secret]
      end
      client = Client.named(client_id)
      client&.authenticates?(secret) ? client : invalid_client
    end

    # The client_id the request names, in Authorization: Basic or the body,
    # unchecked; for the limits.
    def asking_client_id
      basic = basic_credentials
      basic.is_a?(Array) ? basic[0] : params[:client_id].to_s.presence
    end

    # client_secret_basic (RFC 6749 §2.3.1): the form-encoded client_id and
    # secret, or :malformed (also when a part is not UTF-8); nil with no
    # Basic header.
    def basic_credentials
      scheme, value = request.authorization.to_s.split(" ", 2)
      return unless scheme&.casecmp?("basic")

      parts = Base64.strict_decode64(value.to_s).split(":", 2).then { |id, secret| [ id.to_s, secret.to_s ] }
        .map { |part| URI.decode_www_form_component(part) }
      parts.all?(&:valid_encoding?) ? parts : :malformed
    rescue ArgumentError
      :malformed
    end

    # 401 invalid_client (RFC 6749 §5.2), and nil.
    def invalid_client
      response.set_header("WWW-Authenticate", 'Basic realm="SolidTrace"')
      oauth_error("invalid_client", INVALID_CLIENT, status: :unauthorized)
      nil
    end

    # An OAuth error answer (OAuth 2.1 §3.2.4): error, and a description that
    # says what to do.
    def oauth_error(error, description, status: :bad_request)
      render json: { error: error, error_description: description }, status: status
    end
  end
end
