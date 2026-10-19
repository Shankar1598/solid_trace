# frozen_string_literal: true

class OrganizationSerializer
  # invite_urls_from is the Console URL that Invited members' invite_url
  # starts with; leave it out for anyone but an admin, who alone invites.
  def initialize(organization, include_members: false, invite_urls_from: nil)
    @organization = organization
    @include_members = include_members
    @invite_urls_from = invite_urls_from
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
        member[:invite_url] = @invite_urls_from + ou.user.invite_path(@organization) if @invite_urls_from && ou.user.invited?
        member
      end
    end

    result
  end

  def to_json(*)
    as_json.to_json
  end
end
