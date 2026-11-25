# frozen_string_literal: true

class AddRunFilesToFactoryDashboardQueries < ActiveRecord::Migration[8.1]
  class Chart < ActiveRecord::Base
    self.table_name = "dashboard_charts"
  end

  class Project < ActiveRecord::Base
    self.table_name = "projects"
  end

  V0_23_QUERIES = [
    <<~QUERY.chomp,
      where event.kind == "closed" and factory.project == $project and run.close_reason == "pr_merged"
      | extend used = totals.tokens.input + totals.tokens.output + totals.tokens.cache_read + totals.tokens.cache_write
      | distinct time, run.id, run.seq, used
      | summarize tokens = avg(used) by bin_auto(time)
    QUERY
    <<~QUERY.chomp,
      where event.kind == "closed" and factory.project == $project and run.close_reason == "pr_merged"
      | extend seconds = totals.seconds_to_pr_opened + totals.seconds_from_pr_opened_to_outcome
      | distinct time, run.id, run.seq, seconds
      | summarize cycle_hours = avg(seconds) / 3600 by bin_auto(time)
    QUERY
    <<~QUERY.chomp,
      where event.kind == "closed" and factory.project == $project and (run.close_reason == "pr_merged" or run.close_reason == "pr_declined")
      | extend outcome = run.close_reason
      | distinct time, run.id, run.seq, outcome
      | summarize acceptance = 100 * countif(outcome == "pr_merged") / (countif(outcome == "pr_merged") + countif(outcome == "pr_declined")) by bin_auto(time)
    QUERY
    <<~QUERY.chomp
      where event.kind == "closed" and factory.project == $project
      | extend total = totals.interventions.total
      | distinct time, run.id, run.seq, total
      | summarize interventions = avg(total) by bin_auto(time)
    QUERY
  ].freeze

  TITLES = [ "Tokens for each merged PR", "Cycle time of merged PRs", "Acceptance rate", "Interventions for each Run" ].freeze

  def change
    up_only do
      charts = Chart.where(project_id: Project.where(agent: true).select(:id))
      charts.where(query: V0_23_QUERIES).find_each do |chart|
        chart.update_columns(query: chart.query.sub("where ", %(where file startswith "runs/" and )))
      end
      charts.where(title: TITLES).where.not("query LIKE ?", 'where file startswith "runs/" and %').find_each do |chart|
        say "Left as a person changed it: chart #{chart.id} (#{chart.title}) of Dashboard #{chart.dashboard_id}. " \
            'Add `file startswith "runs/"` to its Query by hand.'
      end
    end
  end
end
