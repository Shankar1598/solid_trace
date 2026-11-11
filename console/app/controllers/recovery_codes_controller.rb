# frozen_string_literal: true

# Recovery codes, outside any Organization's pages: a new set is shown once,
# right after it's made (ShowsRecoveryCodes), and making a new set, which
# replaces the old, needs a second-step answer in the same request.
# Rate-limited as signing in is.
class RecoveryCodesController < ApplicationController
  include ShowsRecoveryCodes

  layout "dashboard"
  rate_limit to: 10, within: 3.minutes, only: :create,
    with: -> { redirect_back_or_to user_settings_path, alert: "Too many tries. Try again in a few minutes." }

  def show
    shown = flash[:recovery_codes]
    return redirect_to(user_settings_path) unless shown.is_a?(Hash)

    render inertia: "Settings/RecoveryCodes", props: { codes: shown["codes"], continue_to: shown["continue_to"] }
  end

  def create
    if current_user.pass_second_step(params)
      redirect_showing_recovery_codes current_user.make_recovery_codes, continue_to: user_settings_path,
        notice: "New Recovery codes made. The old ones no longer work."
    else
      redirect_back_or_to user_settings_path, alert: "That code didn't work. Try the newest one from your app."
    end
  end
end
