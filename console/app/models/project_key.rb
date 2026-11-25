# frozen_string_literal: true

class ProjectKey < ApplicationRecord
  AGENT_PROJECT = "The Agent Project's key is for Claude Code's OpenTelemetry only. Ingest accepts it on " \
                  "/v1/logs and /v1/metrics, and keeps only the records of a Captured repository."
  ONE_AGENT_KEY = "The Agent Project can have only one key"

  belongs_to :project

  before_validation :generate_keys, on: :create
  validate :one_agent_key, on: :create

  # The DSN a Sentry SDK sends this Project's Events and Logs with, for
  # ingest reached at +ingest_url+ (as the Logs snippets build it).
  def dsn(ingest_url)
    return if project.agent?

    uri = URI(ingest_url)
    host = uri.port == uri.default_port ? uri.host : "#{uri.host}:#{uri.port}"
    "#{uri.scheme}://#{public_key}@#{host}#{uri.path.sub(%r{/+\z}, '')}/#{project_id}"
  end

  # New values in place of the key's, so the old ones no longer send.
  def rotate!
    self.public_key = nil
    self.secret_key = nil
    generate_keys
    save!
  end

  def label
    "Default"
  end

  private

  # A key made without values gets fresh random ones.
  def generate_keys
    self.public_key ||= SecureRandom.hex(16)
    self.secret_key ||= SecureRandom.hex(32)
  end

  def one_agent_key
    errors.add(:base, ONE_AGENT_KEY) if project.agent? && project.project_keys.exists?
  end
end
