# frozen_string_literal: true

class OrganizationSerializer
  # invite_base_url is the Console URL that Invited members' invite_url
  # starts with (User#invite_url); leave it out for anyone but an admin, who
  # alone invites.
  def initialize(organization, include_members: false, invite_base_url: nil)
    @organization = organization
    @include_members = include_members
    @invite_base_url = invite_base_url
  end

  def as_json(*)
    result = {
      id: @organization.id,
      name: @organization.name,
      slug: @organization.slug,
      created_at: @organization.created_at.iso8601,
    }

    if @include_members
      result[:members] = @organization.organization_users.includes(:user).map do |ou|
        member = {
          id: ou.id,
          user: UserSerializer.new(ou.user).as_json,
          role: ou.role,
          invited: ou.user.invited?,
        }
        if @invite_base_url && ou.user.invited?
          member[:invite_url] = ou.user.invite_url(@organization, console_url: @invite_base_url)
        end
        member
      end
    end

    result
  end

  def to_json(*)
    as_json.to_json
  end
end
