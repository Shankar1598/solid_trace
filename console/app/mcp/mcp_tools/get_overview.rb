# frozen_string_literal: true

module McpTools
  # An Organization's Overview, the sections its Overview page shows.
  class GetOverview < Base
    description "Answers an Organization's Overview over today, the last 7 or the last 30 whole UTC days: " \
                "band, the Logs and bytes received (decoded, and as sent) and the bytes ClickHouse stores of the " \
                "range's days, with the compression ratio, unreadable Logs and requests turned away as overloaded; " \
                "rates, Logs, bytes and requests a second, each an average and the busiest second, by arrival time; " \
                "ingest_counts, when counting began. event_store says whether EventStore answered; when it didn't, " \
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
