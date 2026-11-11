# frozen_string_literal: true

# Signing in's second step: after a right password, a user with Two-factor
# gives a code, a Recovery code or an answer from one of their Passkeys here
# (User#pass_second_step says what it accepts) within five minutes, and
# only then gets a Session, with the Remember me they chose. Rate-limited as
# signing in is.
class SecondStepsController < ApplicationController
  include UsesPasskeys

  EXPIRED = "That sign-in took too long. Sign in again."

  layout "auth"
  allow_unauthenticated_access
  rate_limit to: 10, within: 3.minutes, only: :create,
    with: -> { redirect_to second_step_path, alert: "Too many tries. Try again in a few minutes." }
  before_action :set_pending_step
  before_action :refuse_passkey_options_elsewhere, only: :passkey_options

  def new
    render inertia: "Sessions/SecondStep", props: {
      authenticator_app: @user.authenticator_app?,
      # Whether "Use a passkey" can work: a page at the Console URL, and a
      # Passkey of the user's for its host.
      passkey: passkeys_here? && @user.passkeys.exists?(host: passkey_offer.host),
    }
  end

  # Options asking for an answer from one of the pending user's Passkeys.
  def passkey_options
    render json: passkey_proof_options(@user, :second_step)
  end

  def create
    if (accepted = @user.pass_second_step(second_step_answer(:second_step)))
      clear_second_step
      start_new_session_for @user, remember_me: @remember_me
      redirect_to after_authentication_url, notice: (recovery_codes_left_notice if accepted == :recovery_code)
    elsif params[:credential].present?
      redirect_to second_step_path, alert: "That Passkey didn't work. Try again, or use a Recovery code."
    else
      redirect_to second_step_path, alert: "That code didn't work. Try the newest one from your app."
    end
  end

  private

  def recovery_codes_left_notice
    left = @user.recovery_codes_left
    return "Signed in with your last Recovery code. Make new ones on Settings → User." if left.zero?

    "Signed in with a Recovery code. #{left} left."
  end

  def set_pending_step
    @user, @remember_me = pending_second_step
    return if @user

    clear_second_step
    if action_name == "passkey_options"
      render json: { error: EXPIRED }, status: :unprocessable_entity
    else
      redirect_to login_path, alert: EXPIRED
    end
  end
end
