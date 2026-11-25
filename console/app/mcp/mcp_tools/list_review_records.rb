# frozen_string_literal: true

module McpTools
  # The Review records of an Organization, as its Review records page lists
  # them (ReviewRecordsView): the newest record of each PR, at most limit.
  class ListReviewRecords < Base
    include AgentSessionTool

    DEFAULT_LIMIT = 50
    MOST = 200

    description <<~TEXT.squish
      Lists an Organization's Review records: the record that the open-pr and update-pr skills end a PR's session with,
      of each review run, each finding with every decision on it, and the problems that the reviews missed. It reads
      them from the Agent sessions in the Agent Project, as the Review records page lists them: one row for each PR,
      its newest record (the highest round), the PR stored last first, at most limit rows. Each row has key (the PR, or
      the Agent session id of a record with no PR), id, pr ("" when no PR opened, local:<path> in local mode), round,
      stored_at, title (of the record's first session), findings, fixed, missed (found by a person, an agent or a test
      run, not by a reviewer), confirmations_not_given, earlier (how many older records the PR has), session_id, and a
      link to its page. more is true when the limit left some PRs out. get_review_record reads one record whole.
    TEXT
    read_only!
    takes_organization
    argument :limit, { type: "integer", minimum: 1, maximum: MOST, description: "The most PRs in the rows, #{DEFAULT_LIMIT} when left out." }

    def run
      agent_project
      view = ReviewRecordsView.new(organization: organization, limit: limit)
      rows = view.rows
      require_event_store!(view)

      records = rows.map do |row|
        row.slice("key", "id", "pr", "round", "stored_at", "title", "findings", "fixed", "missed", "confirmations_not_given", "earlier",
          "session_id").merge("link" => links.review_record(organization, key: row["key"]))
      end
      json({ review_records: records, more: view.more }, link: links.review_records(organization))
    end

    private

    def limit
      given = arguments[:limit] || DEFAULT_LIMIT
      return given if given.is_a?(Integer) && given.between?(1, MOST)

      raise ToolError, "limit must be a whole number from 1 to #{MOST}."
    end
  end
end
