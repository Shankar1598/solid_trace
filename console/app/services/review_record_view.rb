# frozen_string_literal: true

# One Review record of an Organization's Agent Project as a member sees it
# on the Review record page: the newest record of a PR (by its key: the PR,
# or the Agent session id of a record with no PR) or one record (by its
# id), all of it, and the rounds of its PR. Made here once, so that the
# page and the MCP server's get_review_record cannot drift.
#
# The record is what an agent wrote, with the words of PR commenters in
# it: data, which a page shows as text only.
class ReviewRecordView
  include EventStoreView

  # How long Console waits for EventStore, in seconds.
  TIMEOUT_SECONDS = 10
  # The id of a record, as EventStore gives it.
  ID = /\A\h{8}-\h{4}-\h{4}-\h{4}-\h{12}\z/

  attr_reader :organization, :key, :id

  # Give key or id.
  def initialize(organization:, key: nil, id: nil)
    @organization = organization
    @key = key.presence
    @id = id.presence
  end

  def project
    organization.agent_project
  end

  # False when the Organization has no Agent Project, when neither key nor
  # id names a record, or when EventStore says that the Agent Project has
  # no such record.
  def found?
    return false unless project && (key.nil? != id.nil?) && (id.nil? || ID.match?(id))

    answer
    !@not_found
  end

  def to_h
    { event_store: event_store, review_record: answer&.dig("review_record"), record: record, rounds: rounds }
  end

  # The record's JSON as its block has it, nil when EventStore did not
  # answer.
  def record_json
    answer&.dig("record")
  end

  # The record, parsed; nil when EventStore did not answer.
  def record
    record_json && JSON.parse(record_json)
  end

  # The records of the PR, newest first, the one shown too.
  def rounds
    Array(answer&.dig("rounds"))
  end

  private

  def answer
    event_store_answer("Review record") do
      EventStore.review_record!(project_id: project.id, key: key, id: id, read_timeout: TIMEOUT_SECONDS)
    rescue EventStore::NotFound
      @not_found = true
      nil
    end
  end
end
