# frozen_string_literal: true

class ApplicationController < ActionController::Base
  # Only allow modern browsers supporting webp images, web push, badges, import maps, CSS nesting, and CSS :has.
  allow_browser versions: :modern

  include Authentication
  include CurrentOrganization
  include InertiaShare
  include Pundit::Authorization
  include TimesCalls

  rescue_from Pundit::NotAuthorizedError, with: :user_not_authorized

  before_action :authenticate_user!

  private

  # Console's URL, for the links a page hands out (PublicUrls).
  def console_url
    PublicUrls.console_url(request.base_url)
  end

  # Where SDKs and shippers reach ingest, for DSNs and the Logs snippets
  # (PublicUrls).
  def public_ingest_url
    PublicUrls.ingest_url(request.base_url)
  end

  # The install's settings apply to every Organization, so only an instance
  # admin sees or changes them (EmailSettingsController, InstanceSettingsController).
  def require_instance_admin
    raise Pundit::NotAuthorizedError unless current_user.instance_admin?
  end

  def user_not_authorized
    flash[:alert] = "You are not authorized to perform this action."
    redirect_back(fallback_location: root_path)
  end
end
