class IssueFingerprint < ApplicationRecord
  belongs_to :issue

  validates :fingerprint, presence: true, uniqueness: { scope: :issue_id }
end
