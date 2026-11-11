# frozen_string_literal: true

# Signing in's second step: after a right password, a user with Two-factor
# gives a code here (User#pass_second_step says what it accepts) within
# five minutes, and only then gets a Session, with the Remember me they
# chose. Rate-limited as signing in is.
class SecondStepsController < ApplicationController
  EXPIRED = "That sign-in took too long. Sign in again."

  layout "auth"
  allow_unauthenticated_access
  rate_limit to: 10, within: 3.minutes, only: :create,
    with: -> { redirect_to second_step_path, alert: "Too many tries. Try again in a few minutes." }
  before_action :set_pending_step

  def new
    render inertia: "Sessions/SecondStep"
  end

  def create
    if (accepted = @user.pass_second_step(params))
      clear_second_step
      start_new_session_for @user, remember_me: @remember_me
      redirect_to after_authentication_url, notice: (recovery_codes_left_notice if accepted == :recovery_code)
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
    redirect_to login_path, alert: EXPIRED
  end
end
