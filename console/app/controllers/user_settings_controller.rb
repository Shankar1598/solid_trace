# frozen_string_literal: true

class UserSettingsController < ApplicationController
  include UsesPasskeys

  layout "dashboard"
  before_action :set_current_org, if: -> { params[:org_slug] }

  def show
    render inertia: "Settings/User", props: {
      user: UserSerializer.new(current_user).as_json,
      # Two-factor: the Authenticator app once set up, never its secret.
      authenticator_app: (current_user.authenticator_app? ? { set_up_at: current_user.authenticator_confirmed_at.iso8601 } : nil),
      recovery_codes_left: (current_user.recovery_codes_left if current_user.two_factor?),
      # Where Passkeys are offered and whether here, and the user's Passkeys
      # for that host.
      passkey_offer: passkey_offer_prop,
      passkeys: current_user.passkeys.where(host: passkey_offer.host).order(:created_at).map { |passkey|
        { id: passkey.id, name: passkey.name, created_at: passkey.created_at.iso8601, last_used_at: passkey.last_used_at&.iso8601 }
      },
    }
  end

  def update
    if current_user.update(user_params)
      redirect_back_or_to settings_page, notice: "User settings updated"
    else
      redirect_back_or_to settings_page, inertia: { errors: current_user.errors.to_hash }
    end
  end

  private

  def user_params
    params.require(:user).permit(:name)
  end

  # The page is served with and without an Organization.
  def settings_page
    @current_org ? user_path(org_slug: @current_org.slug) : user_settings_path
  end
end
