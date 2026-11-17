# frozen_string_literal: true

class OrganizationUser < ApplicationRecord
  self.table_name = "organizations_users"

  include Discard::Model

  belongs_to :organization
  belongs_to :user
  enum :role, { member: 0, admin: 1 }

  has_many :comments, dependent: :restrict_with_error
  has_many :assigned_issues, class_name: "Issue", foreign_key: :assignee_id, dependent: :restrict_with_error

  validates :user_id, uniqueness: { scope: :organization_id, message: "is already a member of this organization" }
  validates :role, presence: true
  validate :organization_must_have_at_least_one_admin

  # A removed member's Grants for the Organization stop now, and stay
  # revoked if the member is added again (undiscard).
  after_discard { Oauth::Grant.where(user_id: user_id, organization_id: organization_id).revoke_all! }

  def admin?
    role == "admin"
  end

  private

  def organization_must_have_at_least_one_admin
    return unless role_changed? && role_was == "admin"

    if organization.organization_users.where(role: "admin").where.not(id: id).kept.count.zero?
      errors.add(:role, "must have at least one admin in the organization")
    end
  end
end
