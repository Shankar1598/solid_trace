class Project < ApplicationRecord
  belongs_to :organization
  has_many :issues, dependent: :destroy
  has_many :project_keys, dependent: :destroy
end
