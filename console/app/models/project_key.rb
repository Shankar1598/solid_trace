# frozen_string_literal: true

class ProjectKey < ApplicationRecord
  belongs_to :project

  before_validation :generate_keys, on: :create

  # The DSN a Sentry SDK sends this Project's Events and Logs with, for
  # ingest reached at +ingest_url+ (as the Logs snippets build it).
  def dsn(ingest_url)
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
end
