# frozen_string_literal: true

class UserSettingsController < ApplicationController
  layout "dashboard"
  before_action :set_organization

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

  def set_organization
    if params[:org_slug]
      @current_org = current_user.organizations.find_by!(slug: params[:org_slug])
    end
  end
end
