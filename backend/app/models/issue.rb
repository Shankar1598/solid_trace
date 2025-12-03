class Issue < ApplicationRecord
  belongs_to :project
  has_many :issue_events, dependent: :destroy
  has_many :issue_fingerprints, dependent: :destroy

  # Compute hash from event attributes
  def self.compute_hash(title:, culprit:, event_type:, fingerprint: nil)
    # Build hash input based on fingerprint template
    if fingerprint.present?
      hash_input = fingerprint.map do |part|
        if part == "{{ default }}"
          # Expand default template
          "#{title}||#{culprit}||#{event_type}"
        else
          part.to_s
        end
      end.join("||")
    else
      # No custom fingerprint, use default
      hash_input = "#{title}||#{culprit}||#{event_type}"
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
  def last_seen_at
    issue_events.maximum(:created_at) || created_at
  end

  def events_count
    issue_events.count
  end

  def users_count
    # Placeholder for now, as user data is in jsonb
    0
  end
end
