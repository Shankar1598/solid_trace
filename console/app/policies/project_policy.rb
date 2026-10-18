# frozen_string_literal: true

# Project settings, such as the Project's Log Retention, and deleting a
# Project are for the Organization's admins.
class ProjectPolicy < ApplicationPolicy
  def update?
    record.organization.organization_users.find_by(user: user)&.admin? || false
  end

  def destroy?
    update?
  end
end
