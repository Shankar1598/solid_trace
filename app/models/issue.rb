# frozen_string_literal: true

class Issue < ApplicationRecord
  belongs_to :project
  has_many :issue_fingerprints, dependent: :delete_all
  has_many :events, through: :issue_fingerprints
  has_many :comments, dependent: :delete_all
  before_create :assign_number

  module STATUS
    UNRESOLVED = :unresolved
    RESOLVED = :resolved
  end

  module KIND
    DEFAULT = :default
    ERROR = :error
    CSP = :csp
  end

  enum :status, {
    STATUS::UNRESOLVED => 0,
    STATUS::RESOLVED => 1,
  }

  enum :kind, {
    KIND::DEFAULT => 0,
    KIND::ERROR => 1,
    KIND::CSP => 2,
  }, prefix: true

  def to_param
    number.to_s
  end

  private

  def assign_number
    self.number = ProjectIssueCounter.next_value_for(project)
  end
end
