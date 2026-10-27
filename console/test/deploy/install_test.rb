# frozen_string_literal: true

require "test_helper"
require "open3"

# What deploy/install writes to deploy/.env, which Compose hands Console:
# among its secrets and settings, the Meta Project's DSN and the share of
# Console's requests and jobs it traces (docs/traces.md). The install's
# functions are run from a copy of it, so its deploy/.env is the copy's.
class InstallTest < ActiveSupport::TestCase
  INSTALL = Rails.root.join("../deploy/install")
  KEY = "0123456789abcdef0123456789abcdef"

  setup do
    @dir = Dir.mktmpdir
    FileUtils.cp(INSTALL, @dir)
  end

  teardown do
    FileUtils.remove_entry(@dir)
  end

  test "a first install writes the Meta Project's DSN, made from its key, and a 5% sample rate" do
    env = write_env

    key = env.fetch("META_PROJECT_KEY")
    assert_match(/\A\h{32}\z/, key)
    assert_equal "http://#{key}@127.0.0.1:4000/0", env["SENTRY_DSN"]
    assert_equal "0.05", env["SENTRY_TRACES_SAMPLE_RATE"]
    assert_equal %w[META_PROJECT_KEY SECRET_KEY_BASE SENTRY_DSN SENTRY_TRACES_SAMPLE_RATE
      SOLIDTRACE_CLICKHOUSE_MEMORY_BYTES SOLIDTRACE_CLICKHOUSE_PASSWORD], env.keys.sort
  end

  test "an install from before gets the DSN of the key it has, and a run again keeps what an operator changed" do
    File.write(env_file, "SECRET_KEY_BASE=abc\nMETA_PROJECT_KEY=#{KEY}\n")

    assert_equal "http://#{KEY}@127.0.0.1:4000/0", write_env["SENTRY_DSN"]

    File.write(env_file, File.read(env_file).sub("SENTRY_TRACES_SAMPLE_RATE=0.05", "SENTRY_TRACES_SAMPLE_RATE=0.5"))
    lines = File.read(env_file).lines
    env = write_env
    assert_equal [ "0.5", KEY, "abc" ], env.values_at("SENTRY_TRACES_SAMPLE_RATE", "META_PROJECT_KEY", "SECRET_KEY_BASE")
    assert_equal lines, File.read(env_file).lines
  end

  private

  def env_file = File.join(@dir, ".env")

  # Runs the install's write_env, and answers deploy/.env as a Hash. Sourced,
  # the install only defines its functions; should it ever do more, sudo and
  # docker stop it before it changes anything.
  def write_env
    script = 'sudo() { exit 97; }; docker() { exit 97; }; source "$1" && write_env'
    output, status = Open3.capture2e("bash", "-c", script, "install", File.join(@dir, "install"))
    assert status.success?, output
    File.read(env_file).lines.to_h { |line| line.chomp.split("=", 2) }
  end
end
