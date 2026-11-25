# frozen_string_literal: true

# The Review records of an Organization as a member sees them on the Review
# records page: the newest record of each PR that the Agent sessions of its
# Agent Project hold (ADR 0024), the PR stored last first, each with its
# round, its counts and how many earlier records its PR has. A record with
# no PR has its Agent session id as its key. Made here once, so that the
# page and the MCP server's list_review_records cannot drift.
class ReviewRecordsView
  include EventStoreView

  # How long Console waits for EventStore, in seconds.
  TIMEOUT_SECONDS = 10

  attr_reader :organization, :limit

  # limit is the most PRs, nil for EventStore's own limit (500), which the
  # page uses.
  def initialize(organization:, limit: nil)
    @organization = organization
    @limit = limit
  end

  def project
    organization.agent_project
  end

  def to_h
    return { on: false } unless project

    { on: true, event_store: event_store, rows: rows, more: more }
  end

  # A row for each PR: its newest record's key, id, place, PR ("" for
  # none), round, stored_at, title, findings, fixed, missed,
  # confirmations_not_given, and earlier.
  def rows
    Array(answer&.dig("review_records"))
  end

  # Whether the limit left some PRs out of the rows.
  def more
    answer&.dig("more") || false
  end

  private

  def answer
    event_store_answer("Review records") do
      EventStore.review_records!(project_id: project.id, limit: limit, read_timeout: TIMEOUT_SECONDS)
    end
  end
end
