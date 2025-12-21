# frozen_string_literal: true

module Maintenance
  class DailyPartitionJob < ApplicationJob
    queue_as :default

    def perform
      adapter_type = ActiveRecord::Base.connection.adapter_name.downcase

      if adapter_type.match?(/postgres/)
        ensure_postgres_partition
      elsif adapter_type.match?(/mysql/)
        ensure_mysql_partition
      elsif adapter_type.match?(/sqlite/)
        Rails.logger.info("Maintenance::DailyPartitionJob: SQLite does not support partitioning. Skipping.")
      else
        Rails.logger.warn("Maintenance::DailyPartitionJob: Unsupported Adapter: #{adapter_type}")
      end
    end

    private

    def ensure_postgres_partition
      next_month = (Time.current + 1.month).beginning_of_month
      partition_name = "events_#{next_month.strftime('%Y_%m')}"
      start_date = next_month.strftime("%Y-%m-%d")
      end_date = (next_month + 1.month).beginning_of_month.strftime("%Y-%m-%d")

      # Check if partition exists
      result = ActiveRecord::Base.connection.execute("SELECT to_regclass('#{partition_name}')")
      if result.first["to_regclass"].nil?
        Rails.logger.info("Maintenance::DailyPartitionJob: Creating partition #{partition_name}")
        ActiveRecord::Base.connection.execute(
          "CREATE TABLE #{partition_name} PARTITION OF events FOR VALUES FROM ('#{start_date}') TO ('#{end_date}')"
        )
      else
        Rails.logger.info("Maintenance::DailyPartitionJob: Partition #{partition_name} already exists")
      end
    end

    def ensure_mysql_partition
      next_month_limit = (Time.current + 2.months).beginning_of_month.strftime("%Y-%m-%d")
      partition_name = "p_#{ (Time.current + 1.month).strftime('%Y%m') }"

      # Check if partition exists
      result = ActiveRecord::Base.connection.execute(<<~SQL)
        SELECT PARTITION_NAME
        FROM information_schema.PARTITIONS
        WHERE TABLE_SCHEMA = DATABASE()
          AND TABLE_NAME = 'events'
          AND PARTITION_NAME = '#{partition_name}'
      SQL

      if result.none?
        Rails.logger.info("Maintenance::DailyPartitionJob: Adding partition for #{next_month_limit}")
        ActiveRecord::Base.connection.execute(
          "ALTER TABLE events ADD PARTITION (PARTITION #{partition_name} VALUES LESS THAN ('#{next_month_limit}'))"
        )
      else
        Rails.logger.info("Maintenance::DailyPartitionJob: Partition #{partition_name} already exists")
      end
    end
  end
end
