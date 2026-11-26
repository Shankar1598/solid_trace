# frozen_string_literal: true

# A screenshot or a video that an agent uploaded to prove a PR. EventStore
# writes the rows and keeps the bytes (ADR 0001); Console only reads them,
# and streams the bytes from EventStore's query API.
class EvidenceFile < ApplicationRecord
  belongs_to :organization
  belongs_to :uploaded_by_user, class_name: "User", optional: true

  # The most files a list shows.
  LIST_LIMIT = 500

  scope :newest_first, -> { order(created_at: :desc, id: :desc) }

  # The files of the Agent sessions session_ids, newest first, as the
  # Agent session and Review record pages list them.
  def self.of_sessions(organization, session_ids)
    organization.evidence_files.where(agent_session_id: session_ids.map(&:downcase)).includes(:uploaded_by_user).newest_first.map(&:listed)
  end

  def readonly?
    true
  end

  def deleted?
    deleted_at.present?
  end

  # The file as a list and its page show it.
  def listed
    {
      id: id, caption: caption, content_type: content_type, byte_size: byte_size, agent_session_id: agent_session_id,
      uploaded_by: uploaded_by_user&.then { |user| user.name.presence || user.email }, created_at: created_at, deleted_at: deleted_at,
    }
  end
end
