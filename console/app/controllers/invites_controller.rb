# frozen_string_literal: true

# An Invite link (User#invite_path): an Invited member sets their name and a
# password, which signs them in to the Organization the link came from, and
# signs out anyone else signed in on this browser. A link works once, for
# seven days, and only while they're Invited and still a member of that
# Organization, so revoking the invite kills it.
class InvitesController < ApplicationController
  EXPIRED = "That invite link has expired or was already used. Ask an admin for a new one."

  layout "auth"
  allow_unauthenticated_access
  rate_limit to: 10, within: 3.minutes, only: :update,
    with: -> { redirect_to invite_path(params[:token], org: params[:org]), alert: "Too many tries. Try again in a few minutes." }
  before_action :resume_session
  before_action :set_invite

  def edit
    render inertia: "Invites/Edit", props: {
      token: params[:token],
      org: @organization.slug,
      invite: {
        email: @user.email,
        name: @user.name,
        organization_name: @organization.name,
        signed_in_as: (current_user.email if current_user && current_user != @user),
      },
    }
  end

  # has_secure_password leaves the password as it is when it is blank, so a
  # blank one is refused here.
  def update
    @user.errors.add(:password, :blank) if params[:password].blank?

    if @user.errors.none? && @user.update(params.permit(:name, :password, :password_confirmation))
      terminate_session
      start_new_session_for @user
      redirect_to issues_path(org_slug: @organization.slug)
    else
      redirect_to invite_path(params[:token], org: @organization.slug), inertia: { errors: @user.errors.to_hash }
    end
  end

  private
    def set_invite
      @organization = Organization.find_by(slug: params[:org].to_s)
      @user = User.find_by_token_for(:invite, params[:token].to_s)
      return if @user&.invited? && @organization&.organization_users&.exists?(user: @user)

      if request.get? || request.head?
        render inertia: "Invites/Edit", props: { expired: EXPIRED }
      else
        redirect_to invite_path(params[:token], org: params[:org])
      end
    end
end
