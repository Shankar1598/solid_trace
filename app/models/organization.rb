class Organization < ApplicationRecord
  has_many :projects, dependent: :destroy
  has_many :organization_users, dependent: :destroy
  has_many :users, through: :organization_users
end
