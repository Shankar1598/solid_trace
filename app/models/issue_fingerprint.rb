# frozen_string_literal: true

class IssueFingerprint < ApplicationRecord
  belongs_to :issue
  belongs_to :project

  validates :fingerprint, presence: true, uniqueness: { scope: :project_id }
end
