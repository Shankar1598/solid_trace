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
    title = data["message"].presence ||
            (data["exception"] && data["exception"]["values"]&.first&.fetch("type", nil)).presence ||
            "Unknown Error"

    # Extract culprit (location/transaction where error occurred)
    culprit = data["culprit"].presence ||
              data["transaction"].presence ||
              (data["exception"] && data["exception"]["values"]&.first&.fetch("module", nil)).presence ||
              "unknown"

    # Extract event kind
    kind = determine_kind(data)

    # Extract custom fingerprint array if provided
    custom_fingerprint = data["fingerprint"]

    # Compute hash for grouping
    hash = Issue.compute_hash(
      title: title,
      culprit: culprit,
      kind: kind,
      fingerprint: custom_fingerprint
    )

    # Find or create issue using the hash
    issue = Issue.find_or_create_by_hash(
      project: project,
      hash: hash,
      attributes: {
        title: title,
        culprit: culprit,
        kind: kind,

        status: 0 # unresolved
      }
    )

    # Extract environment
    environment = data["environment"].presence || "unknown"

    # Create issue event
    event = issue.events.create!(event_data: data, environment: environment)

    # Check notification rules and notify integrations if conditions are met
    newly_created = issue.id_previously_changed?
    IntegrationNotificationJob.perform_later(issue, newly_created)

    {
      issue_id: issue.id,
      event_id: event.id
    }
  end

  def determine_kind(data)
    # Check for CSP report (simplified check, can be expanded)
    return "csp" if data.dig("csp-report").present? || data["logger"] == "csp"

    data["exception"].present? ? "error" : "default"
  end
end
