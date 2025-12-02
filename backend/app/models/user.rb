class User < ApplicationRecord
  has_secure_password

  has_and_belongs_to_many :organizations

  validates :email, presence: true, uniqueness: true, format: { with: URI::MailTo::EMAIL_REGEXP }
  validates :name, presence: true
end
