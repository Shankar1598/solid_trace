# frozen_string_literal: true

class OrganizationUsersController < ApplicationController
  layout "dashboard"
  before_action :set_organization

  def index
    render inertia: "Settings/Members", props: {
      organization: OrganizationSerializer.new(@current_org, include_members: true).as_json,
      can_manage_members: @current_org_user.admin?,
    }
  end

  def search
    query = params[:query].to_s.strip
    members = @current_org.organization_users.includes(:user)

    if query.present?
      members = members.joins(:user).where(
        "users.name LIKE :query OR users.email LIKE :query",
        query: "%#{query}%"
      )
    end

    members = members.order("users.name ASC").limit(20)

    render json: {
      members: members.map do |ou|
        {
          id: ou.id,
          user: UserSerializer.new(ou.user).as_json,
          discarded_at: ou.discarded_at&.iso8601,
        }
      end,
    }
  end

  def create
    invite = MemberInvite.call(organization: @current_org, inviter: current_user,
      email: create_params[:email], name: create_params[:name])
    members = organization_users_path(org_slug: @current_org.slug)

    case invite.outcome
    when :added then redirect_to members, notice: "Member added successfully"
    when :reactivated then redirect_to members, notice: "Member reactivated successfully"
    else redirect_to members, alert: invite.error
    end
  end

  def update
    @organization_user = @current_org.organization_users.find(params[:id])
    authorize @organization_user

    if @organization_user.update(update_params)
      redirect_to organization_users_path(org_slug: @current_org.slug), notice: "Member updated successfully"
    else
      redirect_to organization_users_path(org_slug: @current_org.slug), alert: @organization_user.errors.full_messages.first
    end
  end

  def destroy
    @organization_user = @current_org.organization_users.find(params[:id])
    authorize @organization_user

    # Prevent removing the last member
    if @current_org.organization_users.count <= 1
      redirect_to organization_users_path(org_slug: @current_org.slug), alert: "Cannot remove the last member of the organization"
      return
    end

    @organization_user.discard
    redirect_to organization_users_path(org_slug: @current_org.slug), notice: "Member archived successfully"
  end

  private

  def set_organization
    @current_org = current_user.organizations.find_by!(slug: params[:org_slug])
    @current_org_user = @current_org.organization_users.find_by(user: current_user)
  end

  def create_params
    params.require(:member).permit(:email, :name)
  end

  def update_params
    params.require(:organization_user).permit(:role)
  end
end
