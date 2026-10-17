# frozen_string_literal: true

require "digest"

module Notifiers
  # Builds the Message for a Notification about a Monitor, from the Monitor
  # and what its run saw, which MonitorEvaluation stored in the row's payload:
  # - monitor_firing and monitor_resolved: value (nil for no data), column,
  #   threshold, operator, from and to (the range read)
  # - monitor_matched: logs (up to MonitorEvaluation::MATCHED_LOGS), total,
  #   from and to (for the explorer), and logs_skipped_at when Logs were left
  #   unread before this run
  module MonitorMessage
    # PagerDuty takes dedup keys of at most this many characters.
    MAX_DEDUP_KEY = 255
    # Characters of a Log's message shown on its line.
    MAX_LOG_LINE = 300

    OPERATOR_WORDS = { ">" => "above", ">=" => "at or above", "<" => "below", "<=" => "at or below" }.freeze

    class << self
      def build(row)
        monitor = row.monitor
        payload = row.payload || {}
        case row.kind
        when IntegrationNotification::MONITOR_FIRING then firing(monitor, row.group_key, payload)
        when IntegrationNotification::MONITOR_RESOLVED then resolved(monitor, row.group_key, payload)
        when IntegrationNotification::MONITOR_MATCHED then matched(monitor, payload)
        else raise ArgumentError, "Unknown Monitor notification kind: #{row.kind.inspect}"
        end
      end

      # The same for every message about one group of a Monitor, so a
      # resolve closes what its trigger opened: solid-trace-monitor-<id>,
      # then -<group> for a Monitor with groups, or a digest of the group
      # when that would be too long.
      def dedup_key(monitor_id, group_key)
        base = "solid-trace-monitor-#{monitor_id}"
        return base if group_key.blank?

        key = "#{base}-#{group_key}"
        key.length <= MAX_DEDUP_KEY ? key : "#{base}-#{Digest::SHA256.hexdigest(group_key)}"
      end

      # A group's columns as text: service=api, level=error.
      def group_text(group_key)
        MonitorState.group_of(group_key).map { |name, value| "#{name}=#{value.nil? ? 'null' : value}" }.join(", ")
      end

      # A number as a person reads it: whole numbers without a point, others
      # to at most 4 decimals.
      def number(value)
        return "no data" if value.nil?

        value = value.to_f
        value == value.round ? value.round.to_s : value.round(4).to_s
      end

      private

      def firing(monitor, group_key, payload)
        range = range_text(monitor, payload)
        text = if payload["value"].nil?
          "No data over the last #{range}."
        else
          "#{value_text(payload)} over the last #{range}, #{threshold_text(payload)}."
        end
        threshold_message(monitor, group_key, payload, title: "Firing", text: text, severity: "error", action: "trigger")
      end

      # Resolved because the number is back within the threshold, because the
      # group has no row any more (absent), or because a Monitor that fired
      # for no data has data again (no_data).
      def resolved(monitor, group_key, payload)
        range = range_text(monitor, payload)
        text = if payload["no_data"] && payload["value"].nil?
          "There is data again over the last #{range}."
        elsif payload["no_data"]
          "There is data again over the last #{range}: #{value_text(payload)}, not #{threshold_text(payload)}."
        elsif payload["value"].nil?
          "No rows#{' for this group' if group_key.present?} over the last #{range}, so it is back to ok."
        else
          "#{value_text(payload)} over the last #{range}, no longer #{threshold_text(payload)}."
        end
        threshold_message(monitor, group_key, payload, title: "Resolved", text: text, severity: "info", action: "resolve")
      end

      # count_ was 120
      def value_text(payload)
        "#{payload['column'] || 'the value'} was #{number(payload['value'])}"
      end

      # above the threshold of 100
      def threshold_text(payload)
        "#{OPERATOR_WORDS.fetch(payload['operator'], payload['operator'])} the threshold of #{number(payload['threshold'])}"
      end

      # The span the run read, from its from and to, or else the Monitor's
      # range now.
      def range_text(monitor, payload)
        from, to = payload.values_at("from", "to").map { |value| Time.iso8601(value.to_s) rescue nil }
        span(from && to ? ((to - from) / 60).round : monitor.range_minutes)
      end

      def threshold_message(monitor, group_key, payload, title:, text:, severity:, action:)
        group = group_text(group_key)
        fields = [ [ "Project", monitor.project.name ] ]
        fields << [ "Group", group ] if group.present?
        fields << [ "Value", number(payload["value"]) ]
        fields << [ "Threshold", "#{payload['operator']} #{number(payload['threshold'])}" ]
        fields << [ "Range", "last #{range_text(monitor, payload)}" ]

        Message.new(
          title: [ "#{title}: #{monitor.name}", group.presence && "(#{group})" ].compact.join(" "),
          text: text,
          fields: fields,
          lines: [],
          link: explorer_url(monitor, payload["from"], payload["to"]),
          severity: severity,
          dedup_key: dedup_key(monitor.id, group_key),
          action: action
        )
      end

      def matched(monitor, payload)
        logs = Array(payload["logs"])
        total = payload["total"].to_i
        lines = logs.map { |log| log_line(log) }
        lines << "and #{total - logs.size} more" if total > logs.size

        text = "#{total} #{'Log'.pluralize(total)} matched since the last run."
        if payload["logs_skipped_at"].present?
          text += " Some Logs were not checked: runs timed out #{MonitorEvaluation::TIMEOUTS_BEFORE_SKIPPING} " \
            "times in a row, so the Monitor started again from the Logs ingested at #{time(payload['logs_skipped_at'])}."
        end

        Message.new(
          title: "#{monitor.name}: #{total} #{'Log'.pluralize(total)} matched",
          text: text,
          fields: [ [ "Project", monitor.project.name ], [ "Matched", total.to_s ] ],
          lines: lines,
          link: explorer_url(monitor, payload["from"], payload["to"]),
          severity: "warning",
          dedup_key: dedup_key(monitor.id, ""),
          action: "trigger"
        )
      end

      def log_line(log)
        message = log["message"].to_s.squish.truncate(MAX_LOG_LINE)
        [ time(log["time"]), log["level"]&.upcase, log["service"], message ].compact_blank.join(" ")
      end

      def time(value)
        Time.iso8601(value.to_s).utc.strftime("%Y-%m-%d %H:%M:%S UTC")
      rescue ArgumentError
        value.to_s
      end

      def span(minutes)
        if (minutes % 1440).zero? then "#{minutes / 1440} #{'day'.pluralize(minutes / 1440)}"
        elsif (minutes % 60).zero? then "#{minutes / 60} #{'hour'.pluralize(minutes / 60)}"
        else "#{minutes} #{'minute'.pluralize(minutes)}"
        end
      end

      # The Logs explorer on the Monitor's Project and Query, over [from, to).
      def explorer_url(monitor, from, to)
        params = { org_slug: monitor.organization.slug, host: host, mode: "query", q: monitor.query }
        params.merge!(from: from, to: to) if from.present? && to.present?
        Rails.application.routes.url_helpers.project_logs_url(monitor.project, **params)
      end

      def host
        SolidTrace::Application.config.action_mailer.default_url_options[:host] || "localhost:3000"
      end
    end
  end
end
