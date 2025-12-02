class Issue < ApplicationRecord
  belongs_to :project
  has_many :issue_events, dependent: :destroy
end
