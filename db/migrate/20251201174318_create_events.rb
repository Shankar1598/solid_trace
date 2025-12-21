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
        event_fingerprint_id BIGINT NOT NULL,
        created_at TIMESTAMPTZ NOT NULL,
        uuid UUID NOT NULL,
        payload BYTEA,
        environment VARCHAR DEFAULT 'unknown' NOT NULL,
      #{'  '}
        PRIMARY KEY (event_fingerprint_id, created_at, uuid)
      ) PARTITION BY RANGE (created_at);
    SQL

    # Create initial partition
    current_month = Time.current.strftime('%Y_%m')
    next_month_date = (Time.current + 1.month).beginning_of_month.strftime('%Y-%m-%d')
    execute "CREATE TABLE events_#{current_month} PARTITION OF events FOR VALUES FROM ('MINVALUE') TO ('#{next_month_date}');"

    add_index :events, [ :project_id, :environment ]
  end

  def create_mysql_events
    execute <<~SQL
      CREATE TABLE events (
        project_id BIGINT NOT NULL,
        event_fingerprint_id BIGINT NOT NULL,
        created_at DATETIME(6) NOT NULL,
        uuid BINARY(16) NOT NULL,
        payload MEDIUMBLOB,
        environment VARCHAR(255) DEFAULT 'unknown' NOT NULL,
      #{'  '}
        PRIMARY KEY (event_fingerprint_id, created_at, uuid),
      )#{' '}
      PARTITION BY RANGE COLUMNS(created_at) (
        PARTITION p_initial VALUES LESS THAN ('#{ (Time.current + 1.month).beginning_of_month.strftime('%Y-%m-%d') }')
      );
    SQL

    add_index :events, [ :project_id, :environment ]
  end

  def create_sqlite_events
    create_table :events, id: false, primary_key: [ :event_fingerprint_id, :created_at, :uuid ] do |t|
      t.bigint :project_id, null: false
      t.bigint :event_fingerprint_id, null: false
      t.string :uuid, null: false
      t.datetime :created_at, null: false
      t.string :environment, default: 'unknown', null: false

      t.binary :payload
    end

    add_index :events, [ :project_id, :environment ]
  end
end
