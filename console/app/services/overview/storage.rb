# frozen_string_literal: true

class Overview
  # The Overview's Storage section, for Instance admins only: the whole
  # install's, every Organization's, from EventStore's answer (nil when
  # there's none). ClickHouse's local disk use against the disk limit in
  # effect, with the disk's free space; the bucket; the local disk at steady
  # state, each Project's last 7 days' average a day kept for its Retention
  # (local_cache_hours with a bucket), whether that stays under the limit,
  # and about how many days until the limit at the last 7 days' growth when
  # it doesn't (nil when it stays under, or with no growth); and the old Parquet files from before the switch
  # to ClickHouse, with the day Retention deletes the last of them. Their
  # bucket bytes aren't recorded, so they aren't answered.
  class Storage
    def initialize(answer:)
      @storage = answer&.dig("storage")
    end

    def to_h
      return unless @storage

      limit, local = @storage["disk_limit_bytes"].to_i, @storage["local_bytes"].to_i
      {
        whole_install: true,
        local_bytes: local,
        disk_limit_bytes: limit,
        limit_share: limit.positive? ? (local.to_f / limit).round(4) : nil,
        disk_bytes: @storage["disk_bytes"].to_i,
        free_bytes: @storage["free_bytes"].to_i,
        bucket: @storage["bucket"] == true,
        bucket_bytes: @storage["bucket_bytes"].to_i,
        growth_bytes_per_day: @storage["growth_bytes_per_day"].to_f.round,
        projected_local_bytes: @storage["projected_local_bytes"].to_i,
        stays_under_limit: @storage["stays_under_limit"] == true,
        days_until_limit: @storage["days_until_limit"]&.floor,
        old_files: old_files,
      }
    end

    private

    def old_files
      old = @storage["old_files"]
      return unless old

      {
        files: old["files"].to_i, local_bytes: old["local_bytes"].to_i,
        newest_day: old["newest_day"], passes_retention_on: old["passes_retention_on"],
      }
    end
  end
end
