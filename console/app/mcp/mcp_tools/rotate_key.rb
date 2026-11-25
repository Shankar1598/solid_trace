# frozen_string_literal: true

module McpTools
  # Gives a Project's key new values, so its old ones no longer send. For the
  # Organization's admins.
  class RotateKey < Base
    include ProjectKeyTool

    description "Rotates a Project's key: gives it new values, so its old DSN and Log key (for the Agent Project, " \
                "its key in Claude Code's telemetry settings, which has no DSN) no longer send. Every SDK, shipper " \
                "and Claude Code settings file using it must change to the new ones. Only an Organization admin can."
    destructive!
    takes_key

    def run
      authorize!(key, :rotate?, "rotate a Project's keys")
      key.rotate!

      key_answer(key, note: "The old values no longer send. Change every SDK, shipper and Claude Code settings file that used them.")
    end
  end
end
