# frozen_string_literal: true

module McpTools
  # An Organization's Overview, the sections its Overview page shows.
  class GetOverview < Base
    description "Answers an Organization's Overview over today, the last 7 or the last 30 whole UTC days: " \
                "band, the Logs and bytes received (decoded, and as sent), kept by Log processing and stored by " \
                "ClickHouse of the range's days, with the compression ratio (kept over stored), unreadable Logs, " \
                "Logs the Log transform dropped and requests turned away as overloaded; " \
                "rates, Logs, bytes and requests a second, each an average and the busiest second, by arrival time; " \
                "ingest_counts, when counting began; daily, each UTC day's Logs received by series (a Project each, " \
                "the eighth by creation on folded into Other), bytes received and stored, and Logs a second, average " \
                "and peak; projects, each Project's Logs and bytes received, kept and stored, ratio, share of the bytes " \
                "received, Logs a second, Retention, oldest day and Logs kept of every day, and when its last Log " \
                "arrived (stale when over an hour ago); sources, by_logs the Services and files that sent the most " \
                "Logs and by_bytes those that kept the most bytes, each with its Project, Logs kept, bytes, Logs " \
                "dropped by the Log transform and share. event_store says whether EventStore answered; when it didn't, " \
                "its sections are null."
    read_only!
    takes_organization
    argument :range, { type: "string", enum: Overview::RANGES.keys, description: "today (the default), 7d or 30d." }

    def run
      overview = Overview.new(organization: organization, user: user, range: arguments[:range])
      json(overview.to_h, link: links.overview(organization, overview.range_name))
    end
  end
end
