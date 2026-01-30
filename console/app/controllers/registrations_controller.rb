# frozen_string_literal: true

class RegistrationsController < ApplicationController
  layout "auth"
  skip_before_action :authenticate_user!, only: [ :new, :create ]

  def new
    render inertia: "Registrations/New"
  end

  def create
    @user = User.new(user_params)
    if @user.save
      start_new_session_for @user
      redirect_to root_path, notice: "Welcome! You have signed up successfully."
    else
      redirect_to new_registration_path, inertia: { errors: @user.errors.to_hash }
    end
  end

  private

  def user_params
    params.require(:user).permit(:name, :email, :password, :password_confirmation)
  end
end
