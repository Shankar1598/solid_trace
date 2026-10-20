# frozen_string_literal: true

class User < ApplicationRecord
  include ActionText::Attachable

  has_secure_password

  # An Invite link's token. Setting a password changes the salt, so a link
  # works once, for at most seven days.
  generates_token_for :invite, expires_in: 7.days do
    password_salt.last(10)
  end

  # Setting a password ends being Invited, however it's set. An Invited
  # user's first password, the random one MemberInvite gives them, is set on
  # create, so it doesn't.
  before_update -> { self.invited = false }, if: :will_save_change_to_password_digest?

  has_many :organization_users, -> { kept }, class_name: "OrganizationUser", dependent: :restrict_with_error
  has_many :all_organization_users, class_name: "OrganizationUser", dependent: :restrict_with_error
  has_many :organizations, through: :organization_users

  # A fresh Invite link to organization, for an Invited user, on the Console
  # at console_url. That is the URL the admin reached Console on
  # (request.base_url), as the MCP server's other links are: it is right
  # behind a proxy, on any port and without CONSOLE_URL set. Older links keep
  # working until one is used or they expire.
  def invite_url(organization, console_url:)
    console_url.chomp("/") + invite_path(organization)
  end

  # invite_url's path.
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
