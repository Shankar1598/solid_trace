# frozen_string_literal: true

class OrganizationSettingsController < ApplicationController
  layout "dashboard"
  before_action :set_current_org

  def show
    render inertia: "Settings/Organization", props: {
      organization: OrganizationSerializer.new(@current_org, include_members: true).as_json,
      tab: params[:tab] || "general",
    }
  end

  def update
    if @current_org.update(organization_params)
      redirect_to organization_path(org_slug: @current_org.slug), notice: "Organization settings updated"
    else
      redirect_to organization_path(org_slug: @current_org.slug), inertia: { errors: @current_org.errors.to_hash }
    end
  end

  private

  def organization_params
    params.require(:organization).permit(:name, :slug)
  end
end
