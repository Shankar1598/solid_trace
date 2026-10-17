# frozen_string_literal: true

# The sign-in page's "Forgot your password?": an email is sent a link, the
# link opens a form for a new password, and the new password ends every
# session. Whether a user has the email is never said. A link works for 15
# minutes, and only until the password changes.
class PasswordsController < ApplicationController
  layout "auth"
  allow_unauthenticated_access
  rate_limit to: 10, within: 3.minutes, only: :create,
    with: -> { redirect_to new_password_path, alert: "Too many tries. Try again in a few minutes." }
  before_action :set_user_by_token, only: %i[ edit update ]

  def new
    render inertia: "Passwords/New"
  end

  def create
    if user = User.find_by(email: params[:email].to_s)
      PasswordsMailer.reset(user).deliver_later
    end

    redirect_to login_path, notice: "If a user has that email, it has been sent a link to reset the password."
  end

  def edit
    render inertia: "Passwords/Edit", props: { token: params[:token] }
  end

  # has_secure_password leaves the password as it is when it is blank, so a
  # blank one is refused here.
  def update
    @user.errors.add(:password, :blank) if params[:password].blank?

    if @user.errors.none? && @user.update(params.permit(:password, :password_confirmation))
      @user.sessions.destroy_all
      redirect_to login_path, notice: "Password reset. Sign in with the new one."
    else
      redirect_to edit_password_path(params[:token]), inertia: { errors: @user.errors.to_hash }
    end
  end

  private
    def set_user_by_token
      @user = User.find_by_password_reset_token!(params[:token])
    rescue ActiveSupport::MessageVerifier::InvalidSignature, ActiveRecord::RecordNotFound
      redirect_to new_password_path, alert: "That reset link is wrong or has expired. Ask for another."
    end
end
