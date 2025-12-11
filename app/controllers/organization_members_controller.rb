# frozen_string_literal: true

class OrganizationMembersController < ApplicationController
  layout "dashboard"
  before_action :set_organization

  def create
    @user = User.find_by(email: member_params[:email])
    new_user_created = false
    generated_password = nil

    if @user.nil?
      # Auto-create the user with a random password
      generated_password = SecureRandom.alphanumeric(12)
      @user = User.new(
        email: member_params[:email],
        name: member_params[:name].presence || member_params[:email].split("@").first,
        password: generated_password,
        password_confirmation: generated_password
      )

      unless @user.save
        redirect_to settings_path(org_slug: @current_org.slug), alert: @user.errors.full_messages.first
        return
      end

      new_user_created = true
    end

    @organization_user = @current_org.organization_users.build(user: @user)

    if @organization_user.save
      # Send invitation email to newly created users
      if new_user_created && generated_password
        OrganizationMailer.invitation_email(
          user: @user,
          organization: @current_org,
          password: generated_password,
          invited_by: current_user
        ).deliver_later
      end

      redirect_to settings_path(org_slug: @current_org.slug), notice: "Member added successfully"
    else
      redirect_to settings_path(org_slug: @current_org.slug), alert: @organization_user.errors.full_messages.first
    end
  end

  def destroy
    @organization_user = @current_org.organization_users.find(params[:id])

    # Prevent removing the last member
    if @current_org.organization_users.count <= 1
      redirect_to settings_path(org_slug: @current_org.slug), alert: "Cannot remove the last member of the organization"
      return
    end

    @organization_user.destroy
    redirect_to settings_path(org_slug: @current_org.slug), notice: "Member removed successfully"
  end

  private

  def set_organization
    @current_org = current_user.organizations.find_by!(slug: params[:org_slug])
  end

  def member_params
    params.require(:member).permit(:email, :name)
  end
end
