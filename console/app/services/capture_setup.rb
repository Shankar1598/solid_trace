# frozen_string_literal: true

# What the capture setup page gives an engineer or an Admin: the install
# command of the `solidtrace` CLI and the plugin for the instance's version,
# the managed-settings snippet that installs the plugin on each machine, the
# `login` command for this instance, the captured repositories, and the
# settings that send Claude Code's telemetry to the Agent Project.
#
# The CLI comes from the GitHub release of the instance's version (the
# release job's `cli` job, cli/internal/releasefiles). The plugin comes from
# the marketplace file at the repository root, at the release's tag. An
# instance that runs no release (a development build) gets the newest
# release's CLI and the plugin of main.
module CaptureSetup
  REPOSITORY = "Shankar1598/solid_trace"
  RELEASES = "https://github.com/#{REPOSITORY}/releases".freeze
  # Claude Code clones only these folders of the repository.
  SPARSE_PATHS = [ ".claude-plugin", "cli/plugin" ].freeze
  # A release's tag, as the release workflow builds the images from.
  RELEASE_TAG = /\Av\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?\z/
  # The plugin's install id: <plugin>@<marketplace>.
  MARKETPLACE = "solidtrace"
  PLUGIN = "solidtrace@#{MARKETPLACE}".freeze
  STATUS_COMMAND = "solidtrace capture status"
  # It says "enabled" for the plugin when the install worked.
  PLUGIN_CHECK_COMMAND = "claude plugin list"
  NOT_A_RELEASE = "This instance runs %{version}, which is not a release, so the commands use the CLI of the newest release and the plugin of main."
  KEY_PLACEHOLDER = "<key>"

  module_function

  # The page's props for organization. version is the instance's version;
  # console_url and ingest_url are where the CLI reaches this instance.
  def props(organization:, version:, console_url:, ingest_url:)
    files = release_files(version)
    source = marketplace_source(version)
    project = organization.agent_project
    {
      on: project.present?,
      captured_repositories: project&.captured_repositories || [],
      version: version,
      version_note: (format(NOT_A_RELEASE, version: version) unless release?(version)),
      install_command: "curl -fsSL #{files}/install.sh | sh",
      plugin_commands: plugin_commands(source),
      plugin_check_command: PLUGIN_CHECK_COMMAND,
      managed_settings: JSON.pretty_generate(managed_settings(source)),
      login_command: "solidtrace login --ingest-url #{ingest_url} #{console_url}",
      status_command: STATUS_COMMAND,
      telemetry: project && telemetry(project, source, ingest_url),
    }
  end

  # Each member reads the key, as the keys of other Projects (ADR 0023).
  # OTEL_LOG_USER_PROMPTS and OTEL_LOG_TOOL_DETAILS stay off: Claude Code
  # then sends prompt text and Bash commands, from every repository.
  def telemetry(project, source, ingest_url)
    key = project.project_keys.pick(:public_key)
    env = {
      CLAUDE_CODE_ENABLE_TELEMETRY: "1",
      OTEL_LOGS_EXPORTER: "otlp",
      OTEL_METRICS_EXPORTER: "otlp",
      OTEL_EXPORTER_OTLP_PROTOCOL: "http/protobuf",
      OTEL_EXPORTER_OTLP_ENDPOINT: ingest_url,
      OTEL_EXPORTER_OTLP_HEADERS: "Authorization=Bearer #{key || KEY_PLACEHOLDER}",
      OTEL_METRICS_INCLUDE_REPOSITORY: "1",
    }
    {
      user_settings: JSON.pretty_generate(env: env),
      managed_settings: JSON.pretty_generate(managed_settings(source).merge(env: env)),
      has_key: key.present?,
      project_slug: project.slug,
    }
  end

  def release?(version)
    RELEASE_TAG.match?(version.to_s)
  end

  # The folder of the release's files.
  def release_files(version)
    release?(version) ? "#{RELEASES}/download/#{version}" : "#{RELEASES}/latest/download"
  end

  # The marketplace as Claude Code's settings give it: the repository at the
  # release's tag, or at main (no ref) when the instance runs no release.
  def marketplace_source(version)
    { source: "github", repo: REPOSITORY, ref: (version if release?(version)), sparsePaths: SPARSE_PATHS }.compact
  end

  # Add the marketplace to Claude Code, and install the plugin.
  def plugin_commands(source)
    [
      "claude plugin marketplace add #{[ source[:repo], source[:ref] ].compact.join("#")} --sparse #{source[:sparsePaths].join(" ")}",
      "claude plugin install #{PLUGIN}"
    ]
  end

  # Claude Code's managed settings: the marketplace, and the plugin
  # force-enabled, so a person cannot turn it off. The first start of Claude
  # Code adds the marketplace and gets the plugin, and the plugin runs from
  # the next start.
  def managed_settings(source)
    {
      extraKnownMarketplaces: { MARKETPLACE => { source: source } },
      enabledPlugins: { PLUGIN => true },
    }
  end
end
