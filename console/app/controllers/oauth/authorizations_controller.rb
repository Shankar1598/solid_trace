# frozen_string_literal: true

module Oauth
  # The approval page, where a user lets an OAuth client act as them in one
  # Organization.
  #
  # GET checks the client and its redirect URI first: if either is bad, it
  # shows an error page and never redirects. It then keeps the request as a
  # PendingRequest, whose key alone goes into the Rails session, and with no
  # Session sends the browser only to /login, which comes back to the short
  # path ?request=<key>. With a Session it checks the other parameters (an
  # error there goes back to the client) and shows the page.
  #
  # POST takes only the request's key, the Organization's id and the
  # decision. Approve makes a Grant and a code; both answers go back to the
  # redirect URI that GET checked, never to one read from a parameter.
  class AuthorizationsController < ApplicationController
    layout "auth"
    skip_before_action :authenticate_user!
    before_action :require_oauth_on
    before_action :resume_session, only: :create
    # Approve and Deny: 10 in 3 minutes for each user and for each IP address.
    rate_limit to: 10, within: 3.minutes, only: :create, name: "user",
               by: -> { current_user ? "user:#{current_user.id}" : "ip:#{request.remote_ip}" },
               with: -> { error_page(TOO_MANY_DECISIONS, status: :too_many_requests) }
    rate_limit to: 10, within: 3.minutes, only: :create, name: "ip",
               with: -> { error_page(TOO_MANY_DECISIONS, status: :too_many_requests) }

    UNKNOWN_REQUEST = "This sign-in request is unknown or older than 10 minutes, or was started in another browser. " \
                      "Start the sign-in again from the program."
    LOOPBACK_WARNING = "This program runs on a computer. Approve only if you started it."
    NAME_NOT_CHECKED = "Not checked: the client gave this name"
    REVOKE_HINT = "You can revoke this in Settings → User."
    NO_ORGANIZATION = "You are in no Organization, so there is nothing to approve. Ask an Organization admin to " \
                      "add you, then start the sign-in again."
    TOO_MANY_DECISIONS = "Too many approvals and denials. Try again in a few minutes."
    MAX_STATE = 512
    # An S256 code_challenge: the base64url SHA-256 of the verifier, 43
    # characters (RFC 7636 §4.2).
    S256_CHALLENGE = /\A[A-Za-z0-9_-]{43}\z/

    def show
      pending = params[:request].present? ? PendingRequest.find_in_session(session, params[:request]) : start_request
      return if performed?
      return error_page(UNKNOWN_REQUEST) unless pending

      return send_to_sign_in(pending) unless resume_session

      problem = request_problem(pending)
      return back_to_client(pending, error: problem[0], error_description: problem[1]) if problem

      render inertia: "Oauth/Authorize", props: page_props(pending)
    end

    def create
      pending = PendingRequest.find_in_session(session, params[:request])
      return error_page(UNKNOWN_REQUEST) unless pending
      return send_to_sign_in(pending) unless current_user

      problem = request_problem(pending)
      return back_to_client(pending, error: problem[0], error_description: problem[1]) if problem

      if params[:decision] == "approve"
        approve(pending)
      else
        back_to_client(pending, error: "access_denied", error_description: "The user did not approve the sign-in.")
      end
    end

    private

    def issuer
      @issuer ||= Issuer.current
    end

    def require_oauth_on
      error_page(Issuer::OFF_DESCRIPTION) unless issuer.on?
    end

    # The checked request, kept, or nil after an error page.
    def start_request
      client = Client.find_by(client_id: params[:client_id].to_s)
      return error_page("Console does not know this client. Start the sign-in again from the program.") unless client

      redirect_uri = params[:redirect_uri]
      unless client.redirect_uri_registered?(redirect_uri)
        return error_page("The client did not register this redirect URI, so Console does not send you back to it.")
      end

      PendingRequest.keep!(
        client: client, redirect_uri: redirect_uri,
        response_type: params[:response_type], code_challenge: params[:code_challenge],
        code_challenge_method: params[:code_challenge_method], scope: params[:scope], resource: params[:resource],
        state: params[:state]
      ).tap { |pending| pending.remember(session) }
    end

    # Sign-in (password and second step, or a Passkey) comes back to the
    # short path of the approval page, not to the full authorize URL.
    def send_to_sign_in(pending)
      session[:return_to_after_authenticating] = oauth_authorize_path(request: pending.key)
      redirect_to login_path
    end

    # The first problem with the request's other parameters, as an OAuth
    # error and its description, or nil.
    def request_problem(pending)
      if pending.response_type.blank?
        [ "invalid_request", "Send response_type=code." ]
      elsif pending.response_type != "code"
        [ "unsupported_response_type", "Send response_type=code." ]
      elsif !S256_CHALLENGE.match?(pending.code_challenge.to_s) || pending.code_challenge_method != "S256"
        [ "invalid_request", "Send a PKCE code_challenge (43 characters, base64url) with code_challenge_method=S256." ]
      elsif pending.state.to_s.length > MAX_STATE
        [ "invalid_request", "Send a state of at most #{MAX_STATE} characters." ]
      elsif scopes(pending).nil?
        [ "invalid_scope", "Ask for the scopes in the metadata's scopes_supported: #{Scopes.names.join(', ')}." ]
      elsif resource(pending).nil?
        [ "invalid_target", "Send resource=#{issuer.mcp_resource}, or no resource." ]
      end
    end

    # The Scopes the request asks for (mcp when it names none), or nil when
    # one is unknown.
    def scopes(pending)
      names = pending.scope.to_s.split
      names = [ "mcp" ] if names.empty?
      found = names.uniq.map { |name| Scopes.find(name) }
      found unless found.include?(nil)
    end

    # The resource the code is for: the request's, when a Scope asked for is
    # for it, else the Scopes' own; nil when no Scope is for it.
    def resource(pending)
      resources = scopes(pending).map { |scope| scope.resource(issuer) }.uniq
      return resources.first if pending.resource.blank?

      resources.find { |resource| Issuer.same_resource?(resource, pending.resource) }
    end

    def page_props(pending)
      client = pending.client
      organizations = organizations_last_used_first
      {
        request: pending.key,
        client: {
          name: client.name,
          name_note: NAME_NOT_CHECKED,
          redirect_host: URI(pending.redirect_uri).host,
        },
        loopback_warning: (LOOPBACK_WARNING if client.loopback_only?),
        organizations: organizations.map do |organization|
          {
            id: organization.id, name: organization.name, slug: organization.slug,
            scopes: scopes(pending).map { |scope| format(scope.text, organization: organization.name) },
          }
        end,
        no_organization: (NO_ORGANIZATION if organizations.empty?),
        revoke_hint: REVOKE_HINT,
      }
    end

    # The user's Organizations, the last used one (as `/` opens it) first,
    # the others by name.
    def organizations_last_used_first
      current_user.organizations_by_name.to_a
        .partition { |organization| organization.id == session[:last_organization_id] }.flatten
    end

    def approve(pending)
      organization = current_user.organizations.find_by!(id: params[:organization_id])
      chosen = scopes(pending)
      resource = resource(pending)
      grant = Grant.create!(
        user: current_user, client: pending.client, organization: organization,
        scopes: chosen.map(&:name), resources: chosen.map { |scope| scope.resource(issuer) }.uniq,
        approved_via: "browser", approved_at: Time.current,
        user_agent: request.user_agent.to_s.first(255), ip_address: request.remote_ip
      )
      _code, secret = Code.issue_authorization!(grant: grant, code_challenge: pending.code_challenge,
                                                redirect_uri: pending.redirect_uri, resource: resource)
      back_to_client(pending, code: secret)
    end

    # Sends the browser to the request's checked redirect URI with the
    # answer, state and iss (RFC 9207), and ends the request. An Inertia
    # request gets 409 with X-Inertia-Location, which makes the browser open
    # the URL; a plain one a 302.
    def back_to_client(pending, **answer)
      url = with_query(pending.redirect_uri, **answer, state: pending.state.presence, iss: issuer.url)
      pending.finish(session)
      if request.inertia?
        inertia_location(url)
      else
        redirect_to url, allow_other_host: true
      end
    end

    def with_query(uri, **values)
      parsed = URI(uri)
      query = URI.decode_www_form(parsed.query.to_s) + values.compact.map { |key, value| [ key.to_s, value ] }
      parsed.query = URI.encode_www_form(query)
      parsed.to_s
    end

    def error_page(message, status: :bad_request)
      render inertia: "Oauth/Error", props: { message: message }, status: status
    end
  end
end
