# frozen_string_literal: true

module McpTools
  # One Run, as its Run page shows it (RunDetail).
  class GetRun < Base
    include RunTool

    description <<~TEXT.squish
      Reads one Run of an Organization from its run events in the Agent Project, and links to its Run page.
      run has its fields as its last event shows them: state and reason, Project, repository, folder, base_branch,
      trigger, title, actor, PR (url and outcome), the count of Executions, active, closed and last_event_at.
      executions lists each Execution in order: started_at, ended_at, duration_seconds, the reason it ended,
      exit_code, account, VM and the actor who started it. report is the last report, null when there is none:
      outcome, summary, question, the checks that the factory's tool ran (command, result pass or fail,
      exit_status, time, output_tail) and the Evidence paths. events lists each run event in order, with its kind,
      state, actor, message and details, its other Attributes, which show the interventions.
      list_runs lists the Runs. No tool starts, messages or stops a Run.
    TEXT
    read_only!
    takes_organization
    argument :run, { type: "string", description: "The Run's id. list_runs lists them." }, required: true

    def run
      id = arguments[:run].to_s
      raise ToolError, "#{id} is not a Run id. list_runs lists them." unless RunTool::RUN_ID.match?(id)

      answer = EventStore.run!(project_id: agent_project.id, run_id: id)
      raise ToolError, "No Run #{id} in #{organization.slug}. list_runs lists them." unless answer.is_a?(Hash) && answer["run"]

      json(RunDetail.new(answer).to_h, link: links.run(organization, id))
    end
  end
end
