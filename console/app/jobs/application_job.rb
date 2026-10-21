# frozen_string_literal: true

class ApplicationJob < ActiveJob::Base
  # The Log reads a job makes are timed under its class and id
  # (CallTimings).
  before_perform do |job|
    CallTimings::Context.origin = job.class.name
    CallTimings::Context.request_id = job.job_id
  end

  # Automatically retry jobs that encountered a deadlock
  # retry_on ActiveRecord::Deadlocked

  # Most jobs are safe to ignore if the underlying records are no longer available
  # discard_on ActiveJob::DeserializationError
end
