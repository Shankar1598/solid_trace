class SessionsController < ApplicationController
  layout "auth"
  allow_browser versions: :modern
  skip_before_action :authenticate_user!, only: [ :new, :create ]

  def new
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
