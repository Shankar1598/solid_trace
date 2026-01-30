# frozen_string_literal: true

class UserSettingsController < ApplicationController
  layout "dashboard"
  before_action :set_organization

  def show
    render inertia: "Settings/User", props: {
      user: UserSerializer.new(current_user).as_json,
    }
  end

  def update
    if current_user.update(user_params)
      redirect_back_or_to user_path(org_slug: @current_org&.slug), notice: "User settings updated"
    else
      redirect_back_or_to user_path(org_slug: @current_org&.slug), inertia: { errors: current_user.errors.to_hash }
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
