class Organization < ApplicationRecord
  has_many :projects, dependent: :destroy
  has_and_belongs_to_many :users
end
