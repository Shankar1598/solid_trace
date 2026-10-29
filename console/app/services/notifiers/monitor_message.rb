# frozen_string_literal: true

require "digest"

module Notifiers
  # Builds the Message for a Notification about a Monitor, from the Monitor
  # and what its run saw, which MonitorEvaluation stored in the row's payload:
  # - monitor_firing and monitor_resolved: value (nil for no data), column,
  #   threshold, operator, from and to (the range read); comparing the
  #   change, also compare "change", baseline, change (a percent, nil from a
  #   Baseline of 0), baseline_from and baseline_to; a resolve says absent,
  #   no_data, kind_changed, compare_changed, turned_off, deleted or unlinked
  #   when it has one of those reasons
  # - monitor_matched: logs (up to MonitorEvaluation::MATCHED_LOGS), total,
  #   from and to (for the explorer), and logs_skipped_at when Logs were left
  #   unread before this run
  # Every row also keeps what the message says about its Monitor (about), so
  # it is still delivered once the Monitor is deleted.
  module MonitorMessage
    # PagerDuty takes dedup keys of at most this many characters.
    MAX_DEDUP_KEY = 255
    # Characters of a Log's message shown on its line.
    MAX_LOG_LINE = 300

    OPERATOR_WORDS = { ">" => "above", ">=" => "at or above", "<" => "below", "<=" => "at or below" }.freeze
    CHANGE_WORDS = { "rises" => "a rise", "falls" => "a fall", "changes" => "a change" }.freeze
    COMPARED = { "value" => "the value", "change" => "the change from a Baseline" }.freeze

    class << self
      def build(row)
        payload = row.payload || {}
        monitor = row.monitor ? about(row.monitor) : payload.fetch("monitor")
        case row.kind
        when IntegrationNotification::MONITOR_FIRING then firing(monitor, row.group_key, payload)
        when IntegrationNotification::MONITOR_RESOLVED then resolved(monitor, row.group_key, payload)
        when IntegrationNotification::MONITOR_MATCHED then matched(monitor, payload)
        else raise ArgumentError, "Unknown Monitor notification kind: #{row.kind.inspect}"
        end
      end

      # What a message says about its Monitor: its name, Query and range, and
      # where its Project's explorer is. Read from the Monitor while it
      # exists, so an edit before delivery shows.
      def about(monitor)
        {
          "id" => monitor.id, "name" => monitor.name, "query" => monitor.query,
          "range_minutes" => monitor.range_minutes, "project" => monitor.project.name,
          "project_slug" => monitor.project.slug, "organization_slug" => monitor.organization.slug,
        }
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

      # Repeated matches join one open incident, kept apart from the
      # Monitor's group: a Match monitor changed to a Threshold monitor
      # would otherwise fire into the Match incident left open.
      def matched_dedup_key(monitor_id)
        "solid-trace-monitor-#{monitor_id}-matched"
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

      # A number with its Baseline and change: 120 (baseline 60, +100%).
      def reading_text(value, baseline)
        return number(value) if value.nil? || baseline.nil?

        "#{number(value)} (baseline #{number(baseline)}, #{change_text(value, baseline)})"
      end

      # A change as a signed percent, to at most 1 decimal: +100%, -30%, 0%.
      # From a Baseline of 0 it has no size: up from 0, or down from 0.
      def change_text(value, baseline)
        change = LogMonitor.change(value, baseline)
        return "#{change.positive? ? 'up' : 'down'} from 0" if change.infinite?

        "#{'+' if change.positive?}#{number(change.round(1))}%"
      end

      private

      def firing(monitor, group_key, payload)
        range = range_text(monitor, payload)
        text = if payload["value"].nil?
          "No data over the last #{range}."
        elsif change?(payload)
          "#{value_text(payload)} over the last #{range}, #{from_baseline_text(monitor, payload)}."
        else
          "#{value_text(payload)} over the last #{range}, #{threshold_text(payload)}."
        end
        threshold_message(monitor, group_key, payload, title: "Firing", text: text, severity: "error", action: "trigger")
      end

      # Resolved because the number is back within the threshold, because the
      # group has no row any more (absent), because a Monitor that fired for
      # no data has data again (no_data), because the Monitor now matches
      # Logs instead (kind_changed), because it switched between comparing
      # the value and the change (compare_changed), because it was turned
      # off or deleted, or because this Integration was unlinked from it.
      def resolved(monitor, group_key, payload)
        range = range_text(monitor, payload)
        text = if payload["kind_changed"]
          "The Monitor was changed to match Logs, so it no longer compares a number with the threshold."
        elsif payload["compare_changed"]
          now, before = change?(payload) ? %w[value change] : %w[change value]
          "The Monitor now compares #{COMPARED.fetch(now)} instead of #{COMPARED.fetch(before)}, so its groups start again."
        elsif payload["turned_off"]
          "The Monitor was turned off, so it no longer checks the threshold."
        elsif payload["deleted"]
          "The Monitor was deleted, so it no longer checks the threshold."
        elsif payload["unlinked"]
          "This Integration was unlinked from the Monitor, so it hears no more about it."
        elsif payload["no_data"] && payload["value"].nil?
          "There is data again over the last #{range}."
        elsif payload["no_data"]
          "There is data again over the last #{range}: #{value_text(payload)}, not #{threshold_text(payload)}."
        elsif payload["value"].nil?
          "No rows#{' for this group' if group_key.present?} over the last #{range}, so it is back to ok."
        elsif change?(payload)
          "#{value_text(payload)} over the last #{range}, #{from_baseline_text(monitor, payload)}, " \
            "no longer #{CHANGE_WORDS.fetch(payload['operator'], 'a change')} of #{number(payload['threshold'])}% or more."
        else
          "#{value_text(payload)} over the last #{range}, no longer #{threshold_text(payload)}."
        end
        threshold_message(monitor, group_key, payload, title: "Resolved", text: text, severity: "info", action: "resolve")
      end

      # Whether the run, or the Monitor before a switch, compared the change.
      def change?(payload)
        payload["compare"] == "change"
      end

      # up 100% from 60 the 15 minutes before; up from 0 the 15 minutes
      # before; no change from 0 the 15 minutes before
      def from_baseline_text(monitor, payload)
        value, baseline = payload["value"].to_f, payload["baseline"].to_f
        change = LogMonitor.change(value, baseline)
        how = if change.zero? then "no change"
        elsif change.infinite? then change.positive? ? "up" : "down"
        else "#{change.positive? ? 'up' : 'down'} #{number(change.abs.round(1))}%"
        end
        "#{how} from #{number(baseline)} #{baseline_range_text(monitor, payload)}"
      end

      # Where the Baseline range is, after the range it's compared with: the
      # 15 minutes before.
      def baseline_range_text(monitor, payload)
        "the #{range_text(monitor, payload)} before"
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
        span(from && to ? ((to - from) / 60).round : monitor["range_minutes"].to_i)
      end

      def threshold_message(monitor, group_key, payload, title:, text:, severity:, action:)
        group = group_text(group_key)
        fields = [ [ "Project", monitor["project"] ] ]
        fields << [ "Group", group ] if group.present?
        fields << [ "Value", number(payload["value"]) ]
        if change?(payload)
          fields << [ "Baseline", number(payload["baseline"]) ]
          known = !payload["value"].nil? && !payload["baseline"].nil?
          fields << [ "Change", known ? change_text(payload["value"].to_f, payload["baseline"].to_f) : "no data" ]
          fields << [ "Threshold", "#{payload['operator']} #{number(payload['threshold'])}%" ]
        else
          fields << [ "Threshold", "#{payload['operator']} #{number(payload['threshold'])}" ]
        end
        fields << [ "Range", "last #{range_text(monitor, payload)}" ]
        baseline_link = explorer_url(monitor, payload["baseline_from"], payload["baseline_to"]) if payload["baseline_from"]

        Message.new(
          title: [ "#{title}: #{monitor['name']}", group.presence && "(#{group})" ].compact.join(" "),
          text: text,
          fields: fields,
          lines: [],
          link: explorer_url(monitor, payload["from"], payload["to"]),
          baseline_link: baseline_link,
          severity: severity,
          dedup_key: dedup_key(monitor["id"], group_key),
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
          title: "#{monitor['name']}: #{total} #{'Log'.pluralize(total)} matched",
          text: text,
          fields: [ [ "Project", monitor["project"] ], [ "Matched", total.to_s ] ],
          lines: lines,
          link: explorer_url(monitor, payload["from"], payload["to"]),
          severity: "warning",
          dedup_key: matched_dedup_key(monitor["id"]),
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
        params = { org_slug: monitor["organization_slug"], host: host, mode: "query", q: monitor["query"] }
        params.merge!(from: from, to: to) if from.present? && to.present?
        Rails.application.routes.url_helpers.project_logs_url(monitor["project_slug"], **params)
      end

      def host
        SolidTrace::Application.config.action_mailer.default_url_options[:host] || "localhost:3000"
      end
    end
  end
end
