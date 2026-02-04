# frozen_string_literal: true

class OrganizationUser < ApplicationRecord
  self.table_name = "organizations_users"

   include Discard::Model

  belongs_to :organization
  belongs_to :user

  has_many :comments, dependent: :restrict_with_error
  has_many :assigned_issues, class_name: "Issue", foreign_key: :assignee_id, dependent: :restrict_with_error

  validates :user_id, uniqueness: { scope: :organization_id, message: "is already a member of this organization" }
end
