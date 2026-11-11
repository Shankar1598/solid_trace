# frozen_string_literal: true

class SessionsController < ApplicationController
  include UsesPasskeys

  layout "auth"
  allow_browser versions: :modern
  skip_before_action :authenticate_user!, only: [ :new, :create ]
  limit_tries(only: :create) { login_path }

  def new
    render inertia: "Sessions/New", props: {
      sign_up_allowed: InstanceSetting.current.sign_up_allowed?,
      # Whether "Sign in with a passkey" and the email field's autofill work
      # here (PasskeySignInsController).
      passkey_offer: passkey_offer_prop,
    }
  end

  # A right password signs in a user without Two-factor; one with it gets
  # no Session yet, but the second step.
  def create
    clear_second_step
    if user = User.authenticate_by(email: params[:email], password: params[:password])
      if user.two_factor?
        start_second_step_for user, remember_me: remember_me?
        redirect_to second_step_path
      else
        start_new_session_for user, remember_me: remember_me?
        redirect_to after_authentication_url
      end
    else
      redirect_to login_path, alert: "Try another email address or password."
    end
  end

  def destroy
    terminate_session
    redirect_to login_path
  end
end
