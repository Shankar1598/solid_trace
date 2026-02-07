# frozen_string_literal: true

class UserPolicy < ApplicationPolicy
  def authorize_invite!(to_organization)
    unless to_organization.organization_users.find_by(user: user)&.admin?
      raise Pundit::NotAuthorizedError, "You are not authorized to invite users to this organization"
    end
  end
end
