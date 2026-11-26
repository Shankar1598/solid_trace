# frozen_string_literal: true

module Notifiers
  # The Message of a Monitor status row, for the Organization's Monitor
  # status Integration. Facts is what the row keeps: payload writes it into
  # the row, and build reads it back.
  #
  # Both kinds of one Organization share a PagerDuty dedup key. So "running
  # again" resolves the incident that "not running" opened, once no Monitor
  # is still not running. A "running again" while Monitors are still not
  # running has no PagerDuty event.
  module MonitorStatusMessage
    # The most Monitors that a message lists one by one, so that a mail
    # and a PagerDuty event stay small. Slack lists fewer when the lines
    # are long (SlackNotifier#lines_text).
    MAX_LISTED = 100
    LINK_TEXT = "Open the Monitors"
    TURNED_OFF = "turned_off"
    DELETED = "deleted"

    # One Monitor of a row: since (ISO 8601) and error (the last run's
    # error, or why its runs are held) when it is not running. left is
    # TURNED_OFF or DELETED for a Monitor that does not run again but left
    # the set.
    Listed = Data.define(:id, :name, :project, :since, :error, :left) do
      def initialize(since: nil, error: nil, left: nil, **rest)
        super
      end
    end

    # What a row keeps: its Organization, how many of the Organization's
    # Monitors were on, whether the row lists every one of them, the
    # Monitors, and for "running again" how many Monitors that the
    # Integration was told about still do not run.
    Facts = Data.define(:organization_id, :organization_name, :organization_slug, :enabled, :all, :monitors,
      :still_not_running) do
      def initialize(still_not_running: 0, **rest)
        super
      end
    end

    class << self
      def facts(organization, enabled:, all:, monitors:, still_not_running: 0)
        Facts.new(organization_id: organization.id, organization_name: organization.name,
          organization_slug: organization.slug, enabled: enabled, all: all, monitors: monitors,
          still_not_running: still_not_running)
      end

      # The row's payload: plain Hashes with String keys, as the row's coder
      # gives them back.
      def payload(facts)
        facts.to_h.merge(monitors: facts.monitors.map(&:to_h)).deep_stringify_keys
      end

      def read(payload)
        payload = payload.to_h.symbolize_keys
        monitors = Array(payload[:monitors]).map { |monitor| Listed.new(**monitor.symbolize_keys.slice(*Listed.members)) }
        Facts.new(**payload.slice(*Facts.members), monitors: monitors)
      end

      def build(row)
        facts = read(row.payload)
        case row.kind
        when IntegrationNotification::MONITORS_NOT_RUNNING then not_running(facts)
        when IntegrationNotification::MONITORS_RUNNING_AGAIN then running_again(facts)
        else raise ArgumentError, "Unknown Monitor status notification kind: #{row.kind.inspect}"
        end
      end

      def dedup_key(organization_id)
        "solid-trace-monitors-not-running-#{organization_id}"
      end

      private

      # When all the Organization's Monitors stop together, as in a restart,
      # the first error is given once, not on each Monitor's line.
      def not_running(facts)
        monitors = facts.monitors
        first = monitors.min_by { |monitor| monitor.since.to_s }
        if facts.all
          title = "All #{monitors.size} Monitors are not running"
          text = "Since #{time(first.since)}: #{reason(first)}"
          lines = monitors.map { |monitor| "#{named(monitor)}: #{monitor_url(facts, monitor)}" }
        else
          title = monitors.one? ? "#{first.name} is not running" : "#{monitors.size} Monitors are not running"
          text = "Their runs fail, or are held for more than " \
            "#{LogMonitor::HOLD_MEANS_NOT_RUNNING_AFTER.in_minutes.to_i} minutes"
          lines = monitors.map do |monitor|
            "#{named(monitor)}, since #{time(monitor.since)}: #{reason(monitor)} #{monitor_url(facts, monitor)}"
          end
        end
        message(facts, title: title, lines: lines, severity: "error", action: "trigger",
          text: "#{text.delete_suffix('.')}. They compare nothing and send nothing until their runs work again.",
          fields: [ [ "Not running", "#{monitors.size} of #{facts.enabled} Monitors that are on" ] ])
      end

      def running_again(facts)
        monitors = facts.monitors
        running = monitors.reject(&:left)
        off = monitors.select { |monitor| monitor.left == TURNED_OFF }
        deleted = monitors.select { |monitor| monitor.left == DELETED }
        still = facts.still_not_running
        title = if facts.all then "All #{monitors.size} Monitors are running again"
        elsif running.one? then "#{running.first.name} is running again"
        elsif running.any? then "#{running.size} Monitors are running again"
        elsif monitors.one? then "#{monitors.first.name} was #{left_words(monitors.first)}"
        else "#{monitors.size} Monitors were turned off or deleted"
        end
        text = []
        text << "Their runs compare again." if running.any?
        text << "#{count(off.size, 'was', 'were')} turned off, so #{they(off)} not run." if off.any?
        text << "#{count(deleted.size, 'was', 'were')} deleted, so #{they(deleted)} not run." if deleted.any?
        text << "#{count(still, 'is', 'are')} still not running." if still.positive?
        lines = monitors.map { |monitor| "#{named(monitor)}#{" (#{left_words(monitor)})" if monitor.left}" }
        message(facts, title: title, text: text.join(" "), lines: lines, severity: "info",
          action: still.zero? ? "resolve" : nil,
          fields: [ [ "Running again", running.size.to_s ], [ "Still not running", still.to_s ] ])
      end

      def message(facts, title:, text:, fields:, lines:, severity:, action:)
        Message.new(
          title: title, text: text, fields: [ [ "Organization", facts.organization_name ], *fields ],
          lines: lines.first(MAX_LISTED), more: [ lines.size - MAX_LISTED, 0 ].max,
          link: Rails.application.routes.url_helpers.monitors_url(org_slug: facts.organization_slug,
            **PublicUrls.link_options),
          link_text: LINK_TEXT, severity: severity, dedup_key: dedup_key(facts.organization_id), action: action
        )
      end

      def reason(monitor)
        monitor.error || "the runs fail"
      end

      def left_words(monitor)
        monitor.left == DELETED ? "deleted" : "turned off"
      end

      def count(n, one, many)
        n == 1 ? "1 Monitor #{one}" : "#{n} Monitors #{many}"
      end

      def they(monitors)
        monitors.one? ? "it does" : "they do"
      end

      def named(monitor)
        "#{monitor.name} (#{monitor.project})"
      end

      def monitor_url(facts, monitor)
        Rails.application.routes.url_helpers.monitor_url(monitor.id, org_slug: facts.organization_slug,
          **PublicUrls.link_options)
      end

      def time(value)
        Time.iso8601(value.to_s).utc.strftime("%Y-%m-%d %H:%M UTC")
      rescue ArgumentError
        value.to_s
      end
    end
  end
end
