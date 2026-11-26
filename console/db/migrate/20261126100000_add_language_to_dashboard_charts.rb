# frozen_string_literal: true

# A Dashboard Chart's text is a Query, as every Chart so far has, or a SQL
# query, kept in query as a SQL Monitor keeps it. A Text block has no
# language.
class AddLanguageToDashboardCharts < ActiveRecord::Migration[8.1]
  def up
    add_column :dashboard_charts, :language, :string, default: "query"
    execute "UPDATE dashboard_charts SET language = NULL WHERE viz = 'text'"
  end

  def down
    remove_column :dashboard_charts, :language
  end
end
