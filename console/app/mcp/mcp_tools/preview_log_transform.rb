# frozen_string_literal: true

module McpTools
  # Runs a draft Log transform on a Project's recent arrivals, or on Logs the
  # agent gives, storing nothing, as the Log transform page's preview does.
  class PreviewLogTransform < Base
    include LogTransformTool

    MAX_LOGS = 200
    # How many results the answer holds in full.
    IN_FULL = 20
    # How many of the failures' errors a cut answer lists.
    FAILURES = 10

    description "Runs a draft program on a Project's recent arrivals, the newest Logs of each Service as they arrived, " \
                "before any program; or on up to #{MAX_LOGS} logs you give. Nothing is stored or saved. Answers how many " \
                "Logs the draft kept, dropped and failed, and the first #{IN_FULL} Logs in full: before, and after, " \
                "dropped or the error. When there are more, failures gives each error with how many Logs it failed. " \
                "A draft that doesn't compile answers VRL's errors. Any member can. #{LogTransformTool::HOW_TO_WRITE}"
    read_only!
    takes_project
    argument :program, LogTransformTool::PROGRAM, required: true
    argument :logs, {
      type: "array", items: { type: "object" }, maxItems: MAX_LOGS,
      description: "Logs to run the draft on instead of the recent arrivals, as JSON objects a sender would send, " \
                   "such as {\"message\": \"...\", \"service\": \"sidekiq\"}. They're normalised as Log ingest normalises them first.",
    }

    def run
      preview = EventStore.preview_log_transform(project_id: project.id, program: arguments[:program].to_s, logs: arguments[:logs])
      compile_errors!("The program doesn't compile.", preview["errors"]) if preview["errors"].present?

      results = Array(preview["results"])
      answer = { project: project.slug, logs: results.size, **preview.slice("kept", "dropped", "failed"), results: results.first(IN_FULL) }
      answer[:failures] = failures(results) if results.size > IN_FULL
      json(answer, link: log_transform_link)
    end

    private

    # Each error the results failed with, with how many Logs, most first,
    # then first seen.
    def failures(results)
      results.filter_map { |result| result["error"] }.tally.sort_by.with_index { |(_, logs), seen| [ -logs, seen ] }
        .first(FAILURES).map { |error, logs| { error: error, logs: logs } }
    end
  end
end
