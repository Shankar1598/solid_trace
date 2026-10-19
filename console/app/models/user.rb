# frozen_string_literal: true

class User < ApplicationRecord
  include ActionText::Attachable

  has_secure_password

  # An Invite link's token. Setting a password changes the salt, so a link
  # works once, for at most seven days.
  generates_token_for :invite, expires_in: 7.days do
    password_salt.last(10)
  end

  has_many :organization_users, -> { kept }, class_name: "OrganizationUser", dependent: :restrict_with_error
  has_many :all_organization_users, class_name: "OrganizationUser", dependent: :restrict_with_error
  has_many :organizations, through: :organization_users

  # The path of a fresh Invite link to organization, for an Invited user.
  # Older links keep working until one is used or they expire.
  def invite_path(organization)
    Rails.application.routes.url_helpers.invite_path(generate_token_for(:invite), org: organization.slug)
  end

  # The user's Organizations by name, ignoring case.
  def organizations_by_name
    organizations.order(Arel.sql("LOWER(organizations.name)"))
  end

  # One person, one user: kept trimmed and in lower case, and found in any
  # case, as sign-in and invites look users up by it.
  normalizes :email, with: ->(email) { email.strip.downcase }
  validates :email, presence: true, uniqueness: true, format: { with: URI::MailTo::EMAIL_REGEXP }
  validates :name, presence: true
  has_many :sessions, dependent: :destroy
  has_many :api_tokens, dependent: :delete_all
end
