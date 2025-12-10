# frozen_string_literal: true

class OrganizationUser < ApplicationRecord
  self.table_name = "organizations_users"

  belongs_to :organization
  belongs_to :user

  validates :user_id, uniqueness: { scope: :organization_id, message: "is already a member of this organization" }
end
