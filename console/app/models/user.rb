# frozen_string_literal: true

class User < ApplicationRecord
  include ActionText::Attachable

  has_secure_password

  has_many :organization_users, -> { kept }, class_name: "OrganizationUser", dependent: :restrict_with_error
  has_many :all_organization_users, class_name: "OrganizationUser", dependent: :restrict_with_error
  has_many :organizations, through: :organization_users

  validates :email, presence: true, uniqueness: true, format: { with: URI::MailTo::EMAIL_REGEXP }
  validates :name, presence: true
  has_many :sessions, dependent: :destroy
end
