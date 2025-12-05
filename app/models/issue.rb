class Issue < ApplicationRecord
  belongs_to :project
  has_many :events, dependent: :destroy
  has_many :issue_fingerprints, dependent: :destroy
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
    STATUS::RESOLVED => 1
  }

  enum :kind, {
    KIND::DEFAULT => 0,
    KIND::ERROR => 1,
    KIND::CSP => 2
  }, prefix: true

  private

  def assign_number
    self.number = ProjectIssueCounter.next_value_for(project)
  end

  # Compute hash from event attributes
  def self.compute_hash(title:, culprit:, kind:, fingerprint: nil)
    # Build hash input based on fingerprint template
    if fingerprint.present?
      hash_input = fingerprint.map do |part|
        if part == "{{ default }}"
          # Expand default template
          "#{title}||#{culprit}||#{kind}"
        else
          part.to_s
        end
      end.join("||")
    else
      # No custom fingerprint, use default
      hash_input = "#{title}||#{culprit}||#{kind}"
    end

    Digest::MD5.hexdigest(hash_input)
  end

  # Find or create issue by hash
  def self.find_or_create_by_hash(project:, hash:, attributes:)
    # Look for existing issue with this hash
    issue_fingerprint = IssueFingerprint.joins(:issue)
                          .where(fingerprint: hash, issues: { project_id: project.id })
                          .first

    if issue_fingerprint
      issue = issue_fingerprint.issue
      # Reopen if resolved
      issue.update(status: 0) if issue.status != 0
      issue
    else
      # Create new issue
      issue = project.issues.create!(attributes)
      issue.issue_fingerprints.create!(fingerprint: hash)
      issue
    end
  end
end
