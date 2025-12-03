class OrganizationsController < ApplicationController
  def edit
    @organization = current_organization
  end

  def update
    @organization = current_organization
    if @organization.update(organization_params)
      redirect_to edit_organization_path(org_slug: @organization.slug), notice: "Organization updated successfully"
    else
      render :edit, status: :unprocessable_entity
    end
  end

  def invite
    @organization = current_organization
    email = params[:email]

    if email.blank?
      redirect_to edit_organization_path(org_slug: @organization.slug), alert: "Email cannot be blank"
      return
    end

    begin
      @organization.invite_user(email)
      redirect_to edit_organization_path(org_slug: @organization.slug), notice: "Invitation sent successfully"
    rescue => e
      redirect_to edit_organization_path(org_slug: @organization.slug), alert: "Failed to send invitation: #{e.message}"
    end
  end

  private

  def organization_params
    params.require(:organization).permit(:name)
  end
end
