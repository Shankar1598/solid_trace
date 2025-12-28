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
    event = Event.new(environment: environment, payload: data, project_id: project.id)

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
    exception = data.dig("exception", "values")&.last || data["exception"]
    if exception && (exception["type"] || exception["value"])
      type = exception["type"]
      value = exception["value"]&.to_s&.split("\n")&.first

      if type && value.present?
        "#{type}: #{value}".truncate(250)
      else
        type || value || "Unknown Error"
      end
    else
      data["message"].presence || "Unknown Error"
    end
  end

  def extract_culprit
    culprit = data["culprit"].presence || data["transaction"].presence
    return culprit if culprit

    generate_culprit
  end

  def generate_culprit
    platform = data["platform"]
    exceptions = data.dig("exception", "values") || data["exception"]

    # Handle both dict and list for exceptions (GlitchTip/Sentry compatibility)
    exceptions = exceptions["values"] if exceptions.is_a?(Hash) && exceptions["values"]
    exceptions = Array(exceptions)

    if exceptions.any?
      last_exception = exceptions.last
      return "" if last_exception.dig("mechanism", "synthetic")

      stacktraces = exceptions.map { |e| e["stacktrace"] }.compact.select { |st| st["frames"].present? }
    else
      stacktrace = data["stacktrace"]
      stacktraces = stacktrace && stacktrace["frames"] ? [ stacktrace ] : nil
    end

    culprit = nil
    if stacktraces&.any?
      culprit = get_stacktrace_culprit(stacktraces.last, platform)
    end

    if culprit.blank? && data["request"]
      culprit = data.dig("request", "url")
    end

    culprit&.truncate(250) || ""
  end

  def get_stacktrace_culprit(stacktrace, platform)
    default = nil
    frames = Array(stacktrace["frames"])

    frames.reverse_each do |frame|
      next unless frame

      if frame["in_app"]
        culprit = get_frame_culprit(frame, platform)
        return culprit if culprit.present?
      elsif default.nil?
        default = get_frame_culprit(frame, platform)
      end
    end
    default
  end

  def get_frame_culprit(frame, platform)
    platform = frame["platform"] || platform

    if %w[objc cocoa native].include?(platform)
      return frame["function"] || "?"
    end

    fileloc = frame["filename"] ? "#{frame["filename"]}:#{frame["lineno"]}" : nil
    fileloc ||= frame["module"]
    return "" if fileloc.blank?

    if %w[javascript node].include?(platform)
      "#{frame["function"] || "?"}(#{fileloc})"
    else
      "#{frame["function"] || "?"} in #{fileloc}"
    end
  end

  def determine_kind(data)
    # Check for CSP report (simplified check, can be expanded)
    return "csp" if data.dig("csp-report").present? || data["logger"] == "csp"

    data["exception"].present? ? "error" : "default"
  end

  def find_or_create_issue_with_event(fingerprint_hash:, event:, issue_attributes:)
    retries ||= 0

    # Look for existing event fingerprint with this hash
    issue_fingerprint = IssueFingerprint.find_by(fingerprint: fingerprint_hash, project_id: project.id)

    if issue_fingerprint
      # Existing issue found
      issue = issue_fingerprint.issue
      newly_created = false

      ActiveRecord::Base.transaction do
        # Reopen if resolved
        issue.update!(status: 0) if issue.resolved?

        # Associate event with fingerprint and save
        event.issue_fingerprint = issue_fingerprint
        event.save!
      end

      [ issue, newly_created ]
    else
      # Create new issue, fingerprint, and event together
      newly_created = true

      ActiveRecord::Base.transaction do
        issue = project.issues.create!(issue_attributes)
        issue_fingerprint = issue.issue_fingerprints.create!(fingerprint: fingerprint_hash, project: project)
        event.issue_fingerprint = issue_fingerprint
        event.save!
      end

      [ issue, newly_created ]
    end
  rescue ActiveRecord::RecordNotUnique
    # If we hit a unique constraint (on issue_fingerprints), it means another process created it.
    # Retry to find it.
    if (retries += 1) < 3
      retry
    else
      raise
    end
  end
end
