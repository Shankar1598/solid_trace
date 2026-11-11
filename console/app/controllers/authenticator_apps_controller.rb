# frozen_string_literal: true

# The Authenticator app, outside any Organization's pages: the set-up page
# shows a QR code and the secret as text, a code from the app confirms it,
# and removing it needs a second-step answer in the same request. ?org= names
# one of the user's Organizations to return to once it's set up. Confirming
# and removing are rate-limited as signing in is.
class AuthenticatorAppsController < ApplicationController
  layout "dashboard"
  rate_limit to: 10, within: 3.minutes, only: [ :create, :destroy ],
    with: -> { redirect_back_or_to user_settings_path, alert: "Too many tries. Try again in a few minutes." }
  before_action :set_return_organization, only: [ :new, :create ]

  def new
    if current_user.authenticator_app?
      redirect_to return_url, notice: "Your authenticator app is already set up."
      return
    end

    current_user.set_up_authenticator_app
    render inertia: "Settings/TwoFactorSetUp", props: {
      secret: current_user.authenticator_secret,
      uri: current_user.authenticator_uri,
      qr_code: RQRCode::QRCode.new(current_user.authenticator_uri).as_svg(module_size: 4, use_path: true, viewbox: true).sub(/\A<\?xml[^>]*>/, ""),
      return_organization: @return_organization&.as_json(only: [ :name, :slug ]),
    }
  end

  def create
    if current_user.gaining_two_factor(keep: Current.session) { current_user.confirm_authenticator_app(params[:code]) }
      redirect_to return_url, notice: "Two-factor is on."
    else
      redirect_to new_authenticator_app_path(org: @return_organization&.slug), alert: "That code didn't work. Try the newest one from your app."
    end
  end

  def destroy
    if current_user.pass_second_step(params)
      current_user.remove_authenticator_app
      redirect_back_or_to user_settings_path, notice: "Authenticator app removed. Two-factor is off."
    else
      redirect_back_or_to user_settings_path, alert: "That code didn't work. Try the newest one from your app."
    end
  end

  private

  def set_return_organization
    @return_organization = current_user.organizations.find_by(slug: params[:org].to_s) if params[:org].present?
  end

  def return_url
    @return_organization ? overview_path(org_slug: @return_organization.slug) : user_settings_path
  end
end
