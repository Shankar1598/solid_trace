# frozen_string_literal: true

require "openssl"

module Notifiers
  # Delivers one Notification of a 'Factory' Integration to the Dispatcher's
  # generic signed webhook (W6), which starts a Run, or gives the Active Run
  # of the same dedupe_key. It posts to /webhooks/generic/<sender id> under
  # the Dispatcher's public webhook URL when Settings → Instance has one, else
  # under the Dispatcher URL: never to a URL that a member typed.
  #
  # One request for each row, so for each Issue:
  # - SolidTrace-Delivery: the Notification's id, so a retry gives the Run
  #   that an earlier attempt started
  # - SolidTrace-Timestamp: the Unix time of this attempt
  # - SolidTrace-Signature: "sha256=" and the hex HMAC-SHA256, with the
  #   sender's secret, of the timestamp, a "." and the raw body
  # - the body: project (the slug of the Issue's or the Monitor's Project),
  #   prompt, title, link (the Issue's page, or the Logs explorer), dedupe_key
  #   (solid-trace-issue-<id> for an Issue, solid-trace-monitor-<id>[-<group>]
  #   for a Monitor), and attributes: solidtrace.notification (the kind),
  #   solidtrace.issue_number, or solidtrace.monitor_id and
  #   solidtrace.monitor_group
  #
  # Raises NotConfigured with no Dispatcher URL or no sender, Skipped for a
  # Monitor's resolve (a Run finds a cause; a resolve has none), Refused for
  # a 4xx other than 429, which no retry changes, and DeliveryFailed for a
  # 429, any other status that is not 2xx, a connection error or a timeout.
  class FactoryNotifier
    include HttpDelivery

    # The generic webhook's limits (factory/dispatcher/senders.go), in bytes.
    MAX_PROMPT = 64 * 1024
    MAX_TITLE = 200
    MAX_LINK = 2048
    MAX_ATTRIBUTE = 1024

    def initialize(integration, kind:, notification:, issues: [], message: nil, settings: InstanceSetting.current, **)
      @integration = integration
      @kind = kind
      @notification = notification
      @issue = issues.first
      @message = message
      @settings = settings
    end

    def call
      raise Skipped, "The Factory starts no Run for a Monitor's resolve." if kind == IntegrationNotification::MONITOR_RESOLVED
      raise NotConfigured, "No Dispatcher URL in Settings → Instance" if base_url.blank?
      raise NotConfigured, "No sender at the Dispatcher" if integration.sender_id.blank? || integration.sender_secret.blank?

      body = payload.to_json
      timestamp = Time.current.to_i.to_s
      response = post(webhook_url, body, operator_set: true, headers: {
        "SolidTrace-Timestamp" => timestamp,
        "SolidTrace-Delivery" => notification.id.to_s,
        "SolidTrace-Signature" => "sha256=#{OpenSSL::HMAC.hexdigest('SHA256', integration.sender_secret, "#{timestamp}.#{body}")}",
      })
      return response if response.is_a?(Net::HTTPSuccess)

      reason = "HTTP #{response.code}: #{error_text(response)}"
      code = response.code.to_i
      raise Refused, reason if code.between?(400, 499) && code != 429

      raise DeliveryFailed, reason
    end

    private

    attr_reader :integration, :kind, :notification, :issue, :message, :settings

    def base_url
      settings.dispatcher_webhook_url.presence || settings.dispatcher_url
    end

    def webhook_url
      "#{base_url.chomp('/')}/webhooks/generic/#{ERB::Util.url_encode(integration.sender_id)}"
    end

    def payload
      body = monitor? ? monitor_payload : issue_payload
      body[:prompt] = body[:prompt].truncate_bytes(MAX_PROMPT, omission: "…")
      body[:title] = body[:title].to_s.squish.truncate_bytes(MAX_TITLE, omission: "…")
      body.delete(:link) if body[:link].to_s.bytesize > MAX_LINK
      body[:attributes] = body[:attributes].compact.transform_values { |v| v.to_s.truncate_bytes(MAX_ATTRIBUTE) }
      body
    end

    def monitor?
      IntegrationNotification::MONITOR_KINDS.include?(kind)
    end

    REASONS = {
      IntegrationNotification::ISSUE_CREATED => "It is a new Issue.",
      IntegrationNotification::THRESHOLD_REACHED => "Its Event count passed the threshold of this Integration.",
      IntegrationNotification::ASSIGNMENT_CHANGED => "Its assignee changed.",
    }.freeze

    def issue_payload
      link = issue_url(issue)
      prompt = <<~TEXT
        SolidTrace Issue ##{issue.number} in the Project #{issue.project.name} (#{issue.project.slug}): #{issue.title}
        #{REASONS.fetch(kind, '')}

        Kind: #{issue.kind}
        Culprit: #{issue.culprit.presence || 'none'}
        First seen: #{issue.first_seen_at&.iso8601}
        Last seen: #{issue.last_seen_at&.iso8601}
        Issue page: #{link}
      TEXT
      {
        project: issue.project.slug,
        prompt: prompt,
        title: issue.title,
        link: link,
        dedupe_key: "solid-trace-issue-#{issue.id}",
        attributes: { "solidtrace.notification" => kind, "solidtrace.issue_number" => issue.number },
      }
    end

    def monitor_payload
      about = notification.monitor ? MonitorMessage.about(notification.monitor) : notification.payload.fetch("monitor")
      group = MonitorMessage.group_text(notification.group_key)
      prompt = [
        "SolidTrace Monitor #{about['name']} in the Project #{about['project']} (#{about['project_slug']}): " \
          "#{message.title}",
        message.text,
        "",
        *message.fields.map { |label, value| "#{label}: #{value}" },
        "Query:",
        about["query"].to_s,
        *(message.lines.any? ? [ "", "Logs:", *message.lines ] : []),
        "",
        "Logs explorer: #{message.link}",
      ].join("\n")
      {
        project: about["project_slug"],
        prompt: prompt,
        title: message.title,
        link: message.link,
        dedupe_key: MonitorMessage.dedup_key(about["id"], notification.group_key),
        attributes: { "solidtrace.notification" => kind, "solidtrace.monitor_id" => about["id"],
                      "solidtrace.monitor_group" => group.presence },
      }
    end

    def error_text(response)
      parsed = JSON.parse(response.body.to_s)
      (parsed.is_a?(Hash) && parsed["error"].presence) || response.body.to_s.truncate(500)
    rescue JSON::ParserError
      response.body.to_s.truncate(500)
    end
  end
end
