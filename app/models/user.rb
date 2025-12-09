class User < ApplicationRecord
  include ActionText::Attachable

  has_secure_password

  has_many :organization_users, dependent: :destroy
  has_many :organizations, through: :organization_users

  validates :email, presence: true, uniqueness: true, format: { with: URI::MailTo::EMAIL_REGEXP }
  validates :name, presence: true
  has_many :sessions, dependent: :destroy
  has_many :comments, dependent: :nullify
end
