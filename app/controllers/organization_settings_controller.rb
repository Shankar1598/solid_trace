class OrganizationSettingsController < ApplicationController
  layout "dashboard"
  before_action :set_organization

  def show
  end

  def update
    if @current_org.update(organization_params)
      redirect_to settings_path(org_slug: @current_org.slug), notice: "Organization settings updated"
    else
      render :show, status: :unprocessable_entity
    end
  end

  private

  def set_organization
    @current_org = current_user.organizations.find_by!(slug: params[:org_slug])
  end

  def organization_params
    params.require(:organization).permit(:name)
  end
end
