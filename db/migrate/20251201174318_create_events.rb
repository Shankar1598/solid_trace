# This table is partitioned by created_at to improve performance while qerying and dropping older events
# We are using a composite key instead of a single primary key as created_at needs to be part of the primary key for partitioning
# We are using uuid as the primary key as the AUTO_INCREMENT key will reduce ingest throughput by requiring locks

class CreateEvents < ActiveRecord::Migration[8.1]
  def up
    adapter_type = ActiveRecord::Base.connection.adapter_name.downcase

    if adapter_type.match?(/postgres/)
      create_postgres_events
    elsif adapter_type.match?(/mysql/)
      create_mysql_events
    elsif adapter_type.match?(/sqlite/)
      create_sqlite_events
    else
      raise "Unsupported Adapter: #{adapter_type}"
    end
  end

  def down
    drop_table :events
  end

  private

  def create_postgres_events
    execute <<~SQL
      CREATE TABLE events (
        project_id BIGINT NOT NULL,
        issue_fingerprint_id BIGINT NOT NULL,
        created_at TIMESTAMPTZ NOT NULL,
        uuid UUID NOT NULL,
        payload BYTEA,
        environment VARCHAR DEFAULT 'unknown' NOT NULL,
      #{'  '}
        PRIMARY KEY (issue_fingerprint_id, created_at, uuid)
      ) PARTITION BY RANGE (created_at);
    SQL

    # Create initial partition
    current_month = Time.current.strftime('%Y_%m')
    next_month_date = (Time.current + 1.month).beginning_of_month.strftime('%Y-%m-%d')
    execute "CREATE TABLE events_#{current_month} PARTITION OF events FOR VALUES FROM ('MINVALUE') TO ('#{next_month_date}');"
  end

  def create_mysql_events
    execute <<~SQL
      CREATE TABLE events (
        project_id BIGINT NOT NULL,
        issue_fingerprint_id BIGINT NOT NULL,
        created_at DATETIME(6) NOT NULL,
        uuid BINARY(16) NOT NULL,
        payload MEDIUMBLOB,
        environment VARCHAR(255) DEFAULT 'unknown' NOT NULL,
      #{'  '}
        PRIMARY KEY (issue_fingerprint_id, created_at, uuid),
      )#{' '}
      PARTITION BY RANGE COLUMNS(created_at) (
        PARTITION p_initial VALUES LESS THAN ('#{ (Time.current + 1.month).beginning_of_month.strftime('%Y-%m-%d') }')
      );
    SQL
  end

  def create_sqlite_events
    # We use raw SQL to ensure the WITHOUT ROWID syntax is applied correctly
    # WITHOUT ROWID helps with storing the data physically clustered
    execute <<~SQL
      CREATE TABLE events (
        project_id BIGINT NOT NULL,
        issue_fingerprint_id BIGINT NOT NULL,
        uuid BLOB(16) NOT NULL,
        created_at DATETIME NOT NULL,
        environment TEXT DEFAULT 'unknown' NOT NULL,
        payload BLOB,
        PRIMARY KEY (issue_fingerprint_id, created_at, uuid)
      ) WITHOUT ROWID;
    SQL
  end
end
