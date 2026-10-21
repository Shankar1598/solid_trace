# frozen_string_literal: true

class SessionsController < ApplicationController
  layout "auth"
  allow_browser versions: :modern
  skip_before_action :authenticate_user!, only: [ :new, :create ]
  rate_limit to: 10, within: 3.minutes, only: :create,
    with: -> { redirect_to login_path, alert: "Too many tries. Try again in a few minutes." }

  def new
    render inertia: "Sessions/New", props: { sign_up_allowed: InstanceSetting.current.sign_up_allowed? }
  end

  def create
    if user = User.authenticate_by(email: params[:email], password: params[:password])
      start_new_session_for user
      redirect_to after_authentication_url
    else
      redirect_to login_path, alert: "Try another email address or password."
    end
  end

  def destroy
    terminate_session
    redirect_to login_path
  end
end
