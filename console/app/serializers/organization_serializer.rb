# frozen_string_literal: true

class OrganizationSerializer
  def initialize(organization, include_members: false)
    @organization = organization
    @include_members = include_members
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
        {
          id: ou.id,
          user: UserSerializer.new(ou.user).as_json,
          role: ou.role
        }
      end
    end

    result
  end

  def to_json(*)
    as_json.to_json
  end
end
