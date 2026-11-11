# frozen_string_literal: true

# Signing in with a Passkey alone, from the sign-in page's button or its
# email field's autofill: a discoverable-credential assertion with user
# verification, for the Console URL's host. Success makes the Session at
# once, with the Remember me the page sent, and no code. Rate-limited as
# signing in is.
class PasskeySignInsController < ApplicationController
  include UsesPasskeys

  allow_unauthenticated_access
  rate_limit to: 10, within: 3.minutes, only: :create,
    with: -> { redirect_to login_path, alert: "Too many tries. Try again in a few minutes." }
  before_action :refuse_passkey_options_elsewhere, only: :options

  def options
    options = passkey_offer.relying_party.options_for_authentication(user_verification: "required")
    render json: remember_passkey_challenge(:sign_in, options)
  end

  def create
    clear_second_step
    passkey = passkeys_here? && Passkey.accept(passkey_credential, challenge: take_passkey_challenge(:sign_in), offer: passkey_offer)
    if passkey
      start_new_session_for passkey.user, remember_me: remember_me?
      redirect_to after_authentication_url
    else
      redirect_to login_path, alert: "That Passkey didn't sign you in. Try again, or use your password."
    end
  end
end
