# frozen_string_literal: true

class Organization < ApplicationRecord
  has_many :projects, dependent: :destroy
  has_many :organization_users, -> { kept }, class_name: "OrganizationUser", dependent: :restrict_with_error
  has_many :all_organization_users, class_name: "OrganizationUser", dependent: :restrict_with_error
  has_many :users, through: :organization_users
  has_many :integrations, dependent: :destroy

  def organization_users
    all_organization_users.kept
  end
end
