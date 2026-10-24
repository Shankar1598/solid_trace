# frozen_string_literal: true

module McpTools
  # Saves a Project's Log transform as its newest version, as its page does
  # (LogTransformSave). For the Organization's admins.
  class UpdateLogTransform < Base
    include LogTransformTool

    description "Saves a program as a Project's Log transform, its newest version, or refuses it with VRL's errors " \
                "when it doesn't compile. preview_log_transform first shows what it would do. " \
                "Restore an older version by saving its program, from get_log_transform. " \
                "Only an Organization admin can. #{LogTransformTool::HOW_TO_WRITE}"
    destructive!
    takes_project
    argument :program, LogTransformTool::PROGRAM, required: true

    NOTE = "Log processing runs it on the Logs it processes from now on, within seconds. Logs already stored don't change."

    def run
      authorize!(project, :update?, "change a Project's Log transform")
      save = LogTransformSave.call(project: project, user: user, program: arguments[:program])
      compile_errors!("Not saved: the program doesn't compile.", save.errors) unless save.done?

      answer = { project: project.slug, outcome: save.outcome.to_s }
      answer.merge!(version_json(save.version)) if save.version
      answer[:note] = NOTE if save.outcome == :saved
      json(answer, link: log_transform_link)
    end
  end
end
