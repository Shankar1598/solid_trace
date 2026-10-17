# frozen_string_literal: true

class ApplicationController < ActionController::Base
  # Only allow modern browsers supporting webp images, web push, badges, import maps, CSS nesting, and CSS :has.
  allow_browser versions: :modern

  include Authentication
  include InertiaShare
  include Pundit::Authorization

  rescue_from Pundit::NotAuthorizedError, with: :user_not_authorized

  before_action :authenticate_user!

  private

  # Ingest as SDKs and shippers reach it: PUBLIC_INGEST_URL, or else this
  # host at EventStore's ingest port. The Logs snippets and the Project
  # page's DSN send there.
  def public_ingest_url
    ENV["PUBLIC_INGEST_URL"].presence&.chomp("/") || "#{request.scheme}://#{request.host}:4000"
  end

  def user_not_authorized
    flash[:alert] = "You are not authorized to perform this action."
    redirect_back(fallback_location: root_path)
  end
end
