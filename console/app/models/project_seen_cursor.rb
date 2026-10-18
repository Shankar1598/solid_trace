# frozen_string_literal: true

# Where Event processing has counted a Project's Events up to, in Issues'
# times_seen. EventStore writes it (ADR 0001); Console only deletes it with
# its Project.
class ProjectSeenCursor < ApplicationRecord
  self.primary_key = :project_id

  belongs_to :project
end
