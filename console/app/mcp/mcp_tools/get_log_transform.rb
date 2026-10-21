# frozen_string_literal: true

module McpTools
  # A Project's Log transform, as its page shows it: the program, its recent
  # versions and EventStore's running counts.
  class GetLogTransform < Base
    include LogTransformTool

    VERSIONS = 10

    description "Answers a Project's Log transform: the current program (or an older version's), its version id, who " \
                "saved it and when, and whether it's current; the last #{VERSIONS} versions, newest first, with their " \
                "size in bytes; and EventStore's running_counts for the version it runs now (version_id) since it " \
                "started or that version was saved: Logs processed, dropped and failed, average_micros a Log, and " \
                "compile_error when that version doesn't compile, while every Log is stored as it arrived. " \
                "Read it to learn why a Project's Fields look as they do. #{LogTransformTool::LANGUAGE}"
    read_only!
    takes_project
    argument :version, { type: "integer", description: "A version's id, from versions, for its program. The current one when left out." }

    def run
      current = project.log_transform
      shown = arguments[:version] ? project.log_transform_versions.find(arguments[:version]) : current

      answer = { project: project.slug, program: shown&.program.to_s }
      answer.merge!(version_json(shown), current: shown == current) if shown
      answer[:note] = "The Project has no Log transform: its Logs are stored as they arrive." if answer[:program].blank?
      answer[:versions] = project.log_transform_versions.newest_first.includes(:user).limit(VERSIONS)
        .map { |version| version_json(version).merge(bytes: version.program.bytesize) }
      answer[:running_counts] = EventStore.log_transform_stats(project_id: project.id)
      json(answer, link: log_transform_link)
    end
  end
end
