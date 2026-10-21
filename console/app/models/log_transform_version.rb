# frozen_string_literal: true

# One saved version of a Project's Log transform, a VRL program that Log
# processing runs on each of the Project's Logs (ADR 0012). The newest is the
# Project's current Log transform; an empty program means none. Versions are
# never changed: LogTransformSave saves a new one, and restoring saves an old
# one's program again. user is who saved it, nil once they're gone.
class LogTransformVersion < ApplicationRecord
  # The largest program EventStore runs, as its check refuses a longer one.
  MAX_PROGRAM_BYTES = 64.kilobytes

  belongs_to :project
  belongs_to :user, optional: true

  validate :program_fits

  scope :newest_first, -> { order(id: :desc) }

  private

  def program_fits
    errors.add(:program, "is over 64 KB") if program.to_s.bytesize > MAX_PROGRAM_BYTES
  end
end
