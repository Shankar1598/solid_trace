# frozen_string_literal: true

module Oauth
  # The device page, where a signed-in user types the user code that a
  # program with no browser shows, and approves it for one Organization.
  #
  # The field is always empty: no link fills it, so a link that someone
  # sends cannot make the approval one click (device-code phishing). Wrong
  # codes are slowed down by the limit on entries. After a right code,
  # Console fetches or reads the client's published description
  # (PublishedDescription.device_client); a bad one shows the error page and
  # denies the device code. A good one keeps a PendingRequest for the
  # device code, whose key alone goes into the Rails session, and the page
  # shows the client, where its description is published, and when and
  # from which IP address the program asked, as the approval page does.
  #
  # POST takes the user code, or the request's key, the Organization's id
  # and the decision. The rules of the approval page hold: the request
  # answers only in its own browser, a user in no Organization has only
  # Deny, and Require two-factor sends a user with no Two-factor to the
  # set-up first, which comes back here.
  class DevicesController < ApplicationController
    include ReturnsToOrganization

    layout "auth"
    skip_before_action :authenticate_user!
    before_action :require_oauth_on
    before_action :resume_session
    # Code entries, Approve and Deny: 10 in 3 minutes for each user and for
    # each IP address.
    rate_limit to: 10, within: 3.minutes, only: :create, name: "user",
               by: -> { current_user ? "user:#{current_user.id}" : "ip:#{request.remote_ip}" },
               with: -> { error_page(TOO_MANY, status: :too_many_requests) }
    rate_limit to: 10, within: 3.minutes, only: :create, name: "ip",
               with: -> { error_page(TOO_MANY, status: :too_many_requests) }

    WARNING = "Type a code only from a program that you started yourself."
    WRONG_CODE = "That code is wrong or expired. Check the code in the program, and type it again."
    UNKNOWN_REQUEST = "This sign-in request is unknown, decided or older than 10 minutes, or was started in another " \
                      "browser. Ask the program for a new code."
    TOO_MANY = "Too many codes, approvals and denials. Try again in a few minutes."
    APPROVED = "Approved. Go back to the program: it signs in within a few seconds."
    DENIED = "Denied. The program gets no access."

    def show
      return send_to_sign_in unless current_user
      return render_entry if params[:request].blank?

      pending = waiting_request
      return error_page(UNKNOWN_REQUEST) unless pending

      render inertia: "Oauth/DeviceApproval", props: approval_props(pending)
    end

    def create
      return send_to_sign_in unless current_user
      return enter_code if params[:request].blank?

      pending = waiting_request
      return error_page(UNKNOWN_REQUEST) unless pending

      if params[:decision] == "approve"
        approve(pending)
      else
        deny(pending)
      end
    end

    private

    def issuer
      @issuer ||= Issuer.current
    end

    def require_oauth_on
      error_page(Issuer::OFF_DESCRIPTION) unless issuer.on?
    end

    # Sign-in comes back to the empty device page, never to a URL with a
    # code or a request in it.
    def send_to_sign_in
      session[:return_to_after_authenticating] = device_path
      redirect_to login_path
    end

    def render_entry(error: nil, done: nil, status: :ok)
      render inertia: "Oauth/Device", props: { warning: WARNING, error: error, done: done }, status: status
    end

    def enter_code
      code = DeviceCode.find_waiting(params[:user_code])
      return render_entry(error: WRONG_CODE, status: :unprocessable_entity) unless code

      client = PublishedDescription.device_client(code.client_id, user: current_user)
      code.update!(client: client)
      pending = PendingRequest.keep!(client: client, code: code, scope: code.scopes.join(" "), resource: code.resource)
      pending.remember(session)
      redirect_to device_path(request: pending.key)
    rescue PublishedDescription::TryAgain => problem
      error_page(problem.message)
    rescue PublishedDescription::Problem => problem
      code.deny!
      error_page(problem.message)
    end

    # The request in params, if this browser holds it and its device code
    # still waits for a decision; else nil.
    def waiting_request
      pending = PendingRequest.find_in_session(session, params[:request], device: true)
      pending if pending&.code&.waiting?
    end

    def approval_props(pending)
      code = pending.code
      organizations = organizations_last_used_first
      {
        request: pending.key,
        client: { name: pending.client.name, name_note: "Published at #{pending.client.published_host}" },
        asked_at: code.created_at.iso8601,
        asked_from: code.ip_address,
        organizations: organizations.map do |organization|
          {
            id: organization.id, name: organization.name, slug: organization.slug,
            scopes: Scopes.requested(pending.scope).map { |scope| format(scope.text, organization: organization.name) },
            two_factor_note: (format(AuthorizationsController::TWO_FACTOR_FIRST, organization: organization.name) if
              sets_up_two_factor_first?(organization)),
          }
        end,
        no_organization: (AuthorizationsController::NO_ORGANIZATION if organizations.empty?),
        revoke_hint: AuthorizationsController::REVOKE_HINT,
      }
    end

    # The user's Organizations, the last used one first, as on the approval
    # page.
    def organizations_last_used_first
      current_user.organizations_by_name.to_a
        .partition { |organization| organization.id == session[:last_organization_id] }.flatten
    end

    def approve(pending)
      organization = current_user.organizations.find_by!(id: params[:organization_id])
      return send_to_set_up_two_factor(pending, organization) if sets_up_two_factor_first?(organization)

      scopes = Scopes.requested(pending.scope)
      approved = Grant.transaction do
        grant = Grant.create!(
          user: current_user, client: pending.client, organization: organization,
          scopes: scopes.map(&:name), resources: scopes.map { |scope| scope.resource(issuer) }.uniq,
          approved_via: "device", approved_at: Time.current,
          user_agent: request.user_agent.to_s.first(255), ip_address: request.remote_ip
        )
        pending.code.approve!(grant) || raise(ActiveRecord::Rollback)
      end
      finish(pending)
      approved ? render_entry(done: APPROVED) : error_page(UNKNOWN_REQUEST)
    end

    def deny(pending)
      denied = pending.code.deny!
      finish(pending)
      denied ? render_entry(done: DENIED) : error_page(UNKNOWN_REQUEST)
    end

    # Ends the request, and the kept return to it from a set-up.
    def finish(pending)
      pending.finish(session)
      drop_kept_return(device_path(request: pending.key))
    end

    def sets_up_two_factor_first?(organization)
      organization.require_two_factor && !current_user.two_factor?
    end

    # Require two-factor, as on the approval page: no Grant, but the
    # Authenticator app set-up for the Organization, which comes back to
    # this request's device page once Two-factor is on.
    def send_to_set_up_two_factor(pending, organization)
      keep_return_after_set_up(device_path(request: pending.key), organization)
      redirect_to new_authenticator_app_path(org: organization.slug)
    end

    def error_page(message, status: :bad_request)
      render inertia: "Oauth/Error", props: { message: message }, status: status
    end
  end
end
