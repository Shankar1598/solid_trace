# frozen_string_literal: true

# A Monitor as its pages know it. status is what the list shows first:
# disabled when it is off, firing when any group is firing (its pages note
# last_error beside it), error when its last run failed, and ok otherwise.
# A failed run changes no state, so the groups it left firing still are. include_details adds each group's state,
# the runs it keeps and its Integrations, for its own page.
class MonitorSerializer
  def initialize(monitor, include_details: false)
    @monitor = monitor
    @include_details = include_details
  end

  def as_json(*)
    firing = @monitor.states.count(&:status_firing?)
    result = {
      id: @monitor.id,
      name: @monitor.name,
      kind: @monitor.kind,
      project_id: @monitor.project_id,
      project: { id: @monitor.project.id, name: @monitor.project.name, slug: @monitor.project.slug },
      query: @monitor.query,
      interval_minutes: @monitor.interval_minutes,
      range_minutes: @monitor.range_minutes,
      compare: @monitor.compare,
      baseline: @monitor.baseline,
      minimum: @monitor.minimum,
      operator: @monitor.operator,
      threshold: @monitor.threshold,
      notify_by_group: @monitor.notify_by_group,
      alert_on_no_data: @monitor.alert_on_no_data,
      integration_ids: @monitor.integration_ids,
      enabled: @monitor.enabled?,
      status: status(firing),
      firing_count: firing,
      last_run_at: @monitor.last_run_at&.iso8601,
      last_error: @monitor.last_error,
      logs_skipped_at: @monitor.logs_skipped_at&.iso8601,
      created_at: @monitor.created_at.iso8601,
      updated_at: @monitor.updated_at.iso8601,
    }
    result.merge!(details) if @include_details
    result
  end

  def to_json(*)
    as_json.to_json
  end

  private

  def status(firing)
    if !@monitor.enabled? then "disabled"
    elsif firing.positive? then "firing"
    elsif @monitor.last_error.present? then "error"
    else "ok"
    end
  end

  def details
    {
      states: @monitor.states.sort_by { |s| [ s.status_firing? ? 0 : 1, s.group_key ] }.map do |state|
        { group_key: state.group_key, group: state.group, status: state.status, since: state.since.iso8601,
          last_value: state.last_value, last_baseline: state.last_baseline }
      end,
      runs: @monitor.runs.limit(MonitorRun::KEEP).map do |run|
        {
          id: run.id, ran_at: run.ran_at.iso8601, status: run.status, error: run.error, firing: run.firing,
          matched: run.matched, logs_skipped: run.logs_skipped, elapsed_ms: run.elapsed_ms,
        }
      end,
      integrations: IntegrationOptionSerializer.list(@monitor.integrations.order(:name)),
    }
  end
end
