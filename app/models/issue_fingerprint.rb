# frozen_string_literal: true

class IssueFingerprint < ApplicationRecord
  belongs_to :issue
  belongs_to :project
  has_many :events, dependent: :nullify

  validates :fingerprint, presence: true, uniqueness: { scope: :project_id }
end
