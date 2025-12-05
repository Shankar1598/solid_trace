# Provides atomic, lock-free counter increments for issue numbering.
# Supports PostgreSQL, SQLite (3.35+), and MySQL with DB-specific optimizations.
class ProjectIssueCounter < ApplicationRecord
  self.primary_key = :project_id

  belongs_to :project

  # Returns the next sequential issue number for a project.
  # Uses atomic UPSERT operations to avoid locking.
  def self.next_value_for(project)
    adapter = connection.adapter_name.downcase

    case adapter
    when /postgresql/, /sqlite/
      upsert_with_returning(project)
    when /mysql/
      upsert_mysql(project)
    else
      # Fallback for unknown adapters
      lock_and_increment(project)
    end
  end

  private_class_method

  # PostgreSQL and SQLite 3.35+ support INSERT ... ON CONFLICT ... RETURNING
  def self.upsert_with_returning(project)
    sql = <<~SQL.squish
      INSERT INTO project_issue_counters (project_id, value)
      VALUES (?, 1)
      ON CONFLICT (project_id) DO UPDATE
      SET value = project_issue_counters.value + 1
      RETURNING value
    SQL

    result = connection.exec_query(
      sanitize_sql([ sql, project.id ])
    )
    result.rows.first.first.to_i
  end

  # MySQL lacks atomic RETURNING, use row-level lock instead.
  # Still better than the original approach - only locks this counter row, not the project.
  def self.upsert_mysql(project)
    lock_and_increment(project)
  end

  # Fallback using pessimistic locking for unsupported databases
  def self.lock_and_increment(project)
    transaction do
      counter = find_or_create_by!(project: project)
      counter.with_lock do
        counter.increment!(:value)
        counter.value
      end
    end
  end
end
