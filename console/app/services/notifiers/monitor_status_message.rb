# frozen_string_literal: true

module Notifiers
  # Builds the Message of a Monitor status row, for the Organization's
  # Monitor status Integration, from what IntegrationNotification
  # .monitor_status stored in the row's payload:
  # - organization: id, name and slug
  # - enabled: how many Monitors of the Organization were on
  # - all: every one of them is in the message
  # - monitors: each Monitor's id, name and Project, and
  #   - monitors_not_running: since (ISO 8601) and error (the last run's
  #     error, or why its runs are held)
  #   - monitors_running_again: turned_off, for a Monitor that was turned
  #     off, not one that runs again
  # - still_not_running (monitors_running_again): how many Monitors the
  #   Integration was told about and that still do not run
  #
  # Both kinds of one Organization share a PagerDuty dedup key. So "running
  # again" resolves the incident that "not running" opened, once no Monitor
  # is still not running.
  module MonitorStatusMessage
    # The most Monitors that a message lists one by one.
    MAX_LISTED = 20
    LINK_TEXT = "Open the Monitors"

    class << self
      def build(row)
        payload = row.payload || {}
        case row.kind
        when IntegrationNotification::MONITORS_NOT_RUNNING then not_running(payload)
        when IntegrationNotification::MONITORS_RUNNING_AGAIN then running_again(payload)
        else raise ArgumentError, "Unknown Monitor status notification kind: #{row.kind.inspect}"
        end
      end

      def dedup_key(organization_id)
        "solid-trace-monitors-not-running-#{organization_id}"
      end

      private

      # When all the Organization's Monitors stop together, as in a restart,
      # the first error is given once, not on each Monitor's line.
      def not_running(payload)
        monitors = Array(payload["monitors"])
        first = monitors.min_by { |monitor| monitor["since"].to_s }
        if payload["all"]
          title = "All #{monitors.size} Monitors are not running"
          text = "Since #{time(first['since'])}: #{first['error'] || 'the runs fail'}"
          lines = listed(monitors) { |monitor| "#{named(monitor)}: #{monitor_url(payload, monitor)}" }
        else
          title = monitors.one? ? "#{first['name']} is not running" : "#{monitors.size} Monitors are not running"
          text = "Their runs fail, or are held for more than #{LogMonitor::LONGEST_HOLD.in_minutes.to_i} minutes."
          lines = listed(monitors) do |monitor|
            "#{named(monitor)}, since #{time(monitor['since'])}: #{monitor['error'] || 'the runs fail'} " \
              "#{monitor_url(payload, monitor)}"
          end
        end
        message(payload, title: title, lines: lines, severity: "error", action: "trigger",
          text: "#{text.delete_suffix('.')}. They compare nothing and send nothing until their runs work again.",
          fields: [ [ "Not running", "#{monitors.size} of #{payload['enabled']} Monitors that are on" ] ])
      end

      # It resolves the PagerDuty incident only when no Monitor that the
      # Integration was told about is still not running.
      def running_again(payload)
        monitors = Array(payload["monitors"])
        off, running = monitors.partition { |monitor| monitor["turned_off"] }
        still = payload["still_not_running"].to_i
        title = if payload["all"] then "All #{monitors.size} Monitors are running again"
        elsif running.one? then "#{running.first['name']} is running again"
        elsif running.any? then "#{running.size} Monitors are running again"
        elsif off.one? then "#{off.first['name']} was turned off"
        else "#{off.size} Monitors were turned off"
        end
        text = []
        text << "Their runs compare again." if running.any?
        text << "#{count(off.size, 'was', 'were')} turned off, so #{off.one? ? 'it does' : 'they do'} not run." if off.any?
        text << "#{count(still, 'is', 'are')} still not running." if still.positive?
        lines = listed(monitors) { |monitor| "#{named(monitor)}#{' (turned off)' if monitor['turned_off']}" }
        message(payload, title: title, text: text.join(" "), lines: lines, severity: "info",
          action: still.zero? ? "resolve" : "trigger",
          fields: [ [ "Running again", running.size.to_s ], [ "Still not running", still.to_s ] ])
      end

      def message(payload, title:, text:, fields:, lines:, severity:, action:)
        organization = payload.fetch("organization")
        Message.new(
          title: title, text: text, fields: [ [ "Organization", organization["name"] ], *fields ], lines: lines,
          link: Rails.application.routes.url_helpers.monitors_url(org_slug: organization["slug"], **PublicUrls.link_options),
          link_text: LINK_TEXT, severity: severity, dedup_key: dedup_key(organization["id"]), action: action
        )
      end

      def listed(monitors, &line)
        lines = monitors.first(MAX_LISTED).map(&line)
        lines << "and #{monitors.size - MAX_LISTED} more" if monitors.size > MAX_LISTED
        lines
      end

      def count(n, one, many)
        n == 1 ? "1 Monitor #{one}" : "#{n} Monitors #{many}"
      end

      def named(monitor)
        "#{monitor['name']} (#{monitor['project']})"
      end

      def monitor_url(payload, monitor)
        Rails.application.routes.url_helpers.monitor_url(monitor["id"],
          org_slug: payload.dig("organization", "slug"), **PublicUrls.link_options)
      end

      def time(value)
        Time.iso8601(value.to_s).utc.strftime("%Y-%m-%d %H:%M UTC")
      rescue ArgumentError
        value.to_s
      end
    end
  end
end
