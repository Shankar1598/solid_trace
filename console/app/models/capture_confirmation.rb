# frozen_string_literal: true

# One Admin's confirm of the capture notice (AgentSessionsSettings::NOTICE)
# on the Agent Project: at the turn-on of Agent sessions, or when they added
# captured repositories. entries are the repositories it was for. user is who
# confirmed, nil once they're gone. Rows are never changed.
class CaptureConfirmation < ApplicationRecord
  belongs_to :project
  belongs_to :user, optional: true

  scope :newest_first, -> { order(id: :desc) }
end
