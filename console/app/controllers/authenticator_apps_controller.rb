# frozen_string_literal: true

# The Authenticator app, outside any Organization's pages: the set-up page
# shows a QR code and the secret as text, a code from the app confirms it
# with proof it's the user (User#add_factor), and removing it needs a
# second-step answer in the same request. ?org= names
# one of the user's Organizations to return to once it's set up. Confirming
# and removing are rate-limited as signing in is.
class AuthenticatorAppsController < ApplicationController
  include ShowsRecoveryCodes
  include UsesPasskeys
  include ReturnsToOrganization

  layout "dashboard"
  rate_limit to: 10, within: 3.minutes, only: [ :create, :destroy ],
    with: -> { redirect_back_or_to user_settings_path, alert: "Too many tries. Try again in a few minutes." }

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
      # requires_two_factor says it was the Organization's Require two-factor
      # that sent the user here.
      return_organization: return_organization && {
        name: return_organization.name,
        slug: return_organization.slug,
        requires_two_factor: return_organization.require_two_factor,
      },
      # A Passkey, added on Settings → User, is Two-factor too.
      passkeys_here: passkeys_here?,
      # How confirming proves it's the user (User#add_factor): their
      # password, or, once they have Two-factor (Passkeys), a Recovery code
      # or one of their Passkeys for this host.
      proof: {
        two_factor: current_user.two_factor?,
        passkey: passkeys_here? && current_user.passkeys.exists?(host: passkey_offer.host),
      },
    }
  end

  # Confirming needs proof it's the user too (User#add_factor).
  def create
    if current_user.add_factor(factor_proof, keep_session: Current.session) { current_user.confirm_authenticator_app(params[:code]) }
      redirect_showing_recovery_codes current_user.new_recovery_codes, continue_to: return_url, notice: "Two-factor is on."
    else
      redirect_to new_authenticator_app_path(org: return_organization&.slug), alert: "That code didn't work. Try the newest one from your app."
    end
  rescue User::TwoFactor::WrongProof
    redirect_to new_authenticator_app_path(org: return_organization&.slug), alert: factor_proof_refused
  end

  def destroy
    if current_user.remove_authenticator_app(second_step_answer(:proof))
      notice = current_user.two_factor? ? "Authenticator app removed." : "Authenticator app removed. Two-factor is off."
      redirect_back_or_to user_settings_path, notice: notice
    else
      redirect_back_or_to user_settings_path, alert: second_step_refused
    end
  rescue User::TwoFactor::Required => e
    redirect_back_or_to user_settings_path, alert: e.message
  end
end
