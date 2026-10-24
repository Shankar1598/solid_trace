# frozen_string_literal: true

class Overview
  # The Overview's Storage section, for Instance admins only: the whole
  # install's, every Organization's, from EventStore's answer (nil when
  # there's none). ClickHouse's local disk use against the disk limit in
  # effect, with the disk's free space; the bucket; about how many days until
  # the limit at the last 7 days' growth (nil with a bucket, which Logs move
  # to, or with no growth); and the old Parquet files from before the switch
  # to ClickHouse, with the day Retention deletes the last of them. Their
  # bucket bytes aren't recorded, so they aren't answered.
  class Storage
    def initialize(answer:)
      @storage = answer&.dig("storage")
    end

    def to_h
      return unless @storage

      s = @storage
      limit, local = s["disk_limit_bytes"].to_i, s["local_bytes"].to_i
      {
        whole_install: true,
        local_bytes: local,
        disk_limit_bytes: limit,
        limit_share: limit.positive? ? (local.to_f / limit).round(4) : nil,
        disk_bytes: s["disk_bytes"].to_i,
        free_bytes: s["free_bytes"].to_i,
        bucket: s["bucket"] == true,
        bucket_bytes: s["bucket_bytes"].to_i,
        growth_bytes_per_day: s["growth_bytes_per_day"].to_f.round,
        days_until_limit: s["days_until_limit"]&.floor,
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
