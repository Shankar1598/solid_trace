# frozen_string_literal: true

# A join table between issue and event fingerprints
# This model is named as issue_fingerprint as it has foreign key association with issue and not events
class IssueFingerprint < ApplicationRecord
  belongs_to :issue
  belongs_to :project

  validates :fingerprint, presence: true, uniqueness: { scope: :project_id }
end
