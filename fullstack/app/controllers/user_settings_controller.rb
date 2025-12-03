class UserSettingsController < ApplicationController
  layout "dashboard"

  def show
  end

  def update
    if current_user.update(user_params)
      redirect_to user_settings_path, notice: "User settings updated"
    else
      render :show, status: :unprocessable_entity
    end
  end

  private

  def user_params
    params.require(:user).permit(:name)
  end
end
