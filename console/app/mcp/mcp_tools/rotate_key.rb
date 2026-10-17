# frozen_string_literal: true

module McpTools
  # Gives a Project's key new values, so its old ones no longer send. For the
  # Organization's admins.
  class RotateKey < Base
    include ProjectKeyTool

    description "Rotates a Project's key: gives it new values, so its old DSN and Log key no longer send. " \
                "Every SDK and shipper using it must change to the new ones. Only an Organization admin can."
    destructive!
    takes_key

    def run
      authorize!(key, :rotate?, "rotate a Project's keys")
      key.rotate!

      key_answer(key, note: "The old values no longer send. Change every SDK and shipper that used them.")
    end
  end
end
