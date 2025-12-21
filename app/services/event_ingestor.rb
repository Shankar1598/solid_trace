# frozen_string_literal: true

class EventIngestor
  def initialize(project, data)
    @project = project
    @data = data
  end

  def call
    process_event
  end

  private

  attr_reader :project, :data

  def process_event
    # Extract grouping attributes
    title = extract_title
    culprit = extract_culprit
    kind = determine_kind(data)
    environment = data["environment"].presence || "unknown"
    custom_fingerprint = data["fingerprint"]

    # Compute hash for grouping
    fingerprint_hash = Event.compute_hash(
      title: title,
      culprit: culprit,
      kind: kind,
      fingerprint: custom_fingerprint
    )

    # Build the event (fingerprint will be assigned in find_or_create_issue_with_event)
    event = Event.new(environment: environment)
    event.build_event_payload(payload: data)

    # Find or create issue and save event together in a transaction
    issue, newly_created = find_or_create_issue_with_event(
      fingerprint_hash: fingerprint_hash,
      event: event,
      issue_attributes: {
        title: title,
        culprit: culprit,
        kind: kind,
        status: 0, # unresolved
      }
    )

    # Check notification rules and notify integrations if conditions are met
    IntegrationNotificationJob.perform_later(issue, newly_created)

    {
      issue_id: issue.id,
      event_id: event.id,
    }
  end

  def extract_title
    data["message"].presence ||
      (data["exception"] && data["exception"]["values"]&.first&.fetch("type", nil)).presence ||
      "Unknown Error"
  end

  def extract_culprit
    data["culprit"].presence ||
      data["transaction"].presence ||
      (data["exception"] && data["exception"]["values"]&.first&.fetch("module", nil)).presence ||
      "unknown"
  end

  def determine_kind(data)
    # Check for CSP report (simplified check, can be expanded)
    return "csp" if data.dig("csp-report").present? || data["logger"] == "csp"

    data["exception"].present? ? "error" : "default"
  end

  def find_or_create_issue_with_event(fingerprint_hash:, event:, issue_attributes:)
    retries ||= 0

    # Look for existing event fingerprint with this hash
    event_fingerprint = EventFingerprint.find_by(fingerprint: fingerprint_hash, project_id: project.id)

    if event_fingerprint
      # Existing issue found
      issue = event_fingerprint.issue
      newly_created = false

      ActiveRecord::Base.transaction do
        # Reopen if resolved
        issue.update!(status: 0) if issue.resolved?

        # Associate event with fingerprint and save
        event.event_fingerprint = event_fingerprint
        event.save!
      end

      [ issue, newly_created ]
    else
      # Create new issue, fingerprint, and event together
      newly_created = true

      ActiveRecord::Base.transaction do
        issue = project.issues.create!(issue_attributes)
        event_fingerprint = issue.event_fingerprints.create!(fingerprint: fingerprint_hash, project: project)
        event.event_fingerprint = event_fingerprint
        event.save!
      end

      [ issue, newly_created ]
    end
  rescue ActiveRecord::RecordNotUnique
    # If we hit a unique constraint (on event_fingerprints), it means another process created it.
    # Retry to find it.
    if (retries += 1) < 3
      retry
    else
      raise
    end
  end
end
