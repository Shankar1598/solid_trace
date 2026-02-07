# frozen_string_literal: true

class OrganizationUserPolicy < ApplicationPolicy

  def index?
    true
  end

  def create?
    admin?
  end

  def update?
    admin?
  end

  def destroy?
    admin?
  end

  private

  def admin?
    record.organization.organization_users.find_by(user: user)&.admin?
  end
end
