# frozen_string_literal: true

# What the capture setup page gives an engineer or an Admin: the install
# command of the `solidtrace` CLI and the plugin for the instance's version,
# the managed-settings snippet that installs the plugin on each machine, the
# `login` command for this instance, and the captured repositories.
#
# The files come from the GitHub release of the instance's version (the
# release job's `cli` job, cli/internal/releasefiles). An instance that runs
# no release (a development build) gets the newest release's files.
module CaptureSetup
  RELEASES = "https://github.com/Shankar1598/solid_trace/releases"
  # A release's tag, as the release workflow builds the images from.
  RELEASE_TAG = /\Av\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?\z/
  # The plugin's install id: <plugin>@<marketplace>.
  MARKETPLACE = "solidtrace"
  PLUGIN = "solidtrace@#{MARKETPLACE}".freeze
  STATUS_COMMAND = "solidtrace capture status"
  # Where the plugin commands put the release's marketplace file. Claude Code
  # reads a github.com URL as a git repository, and it does not load a
  # marketplace file added as a plain path. It loads one at
  # <folder>/.claude-plugin/marketplace.json.
  MARKETPLACE_FILE = "~/.local/share/solidtrace/.claude-plugin/marketplace.json"
  # It says "enabled" for the plugin when the install worked.
  PLUGIN_CHECK_COMMAND = "claude plugin list"
  NOT_A_RELEASE = "This instance runs %{version}, which is not a release, so the commands use the newest release."

  module_function

  # The page's props for organization. version is the instance's version;
  # console_url and ingest_url are where the CLI reaches this instance.
  def props(organization:, version:, console_url:, ingest_url:)
    files = release_files(version)
    project = organization.agent_project
    {
      on: project.present?,
      captured_repositories: project&.captured_repositories || [],
      version: version,
      version_note: (format(NOT_A_RELEASE, version: version) unless release?(version)),
      install_command: "curl -fsSL #{files}/install.sh | sh",
      plugin_commands: plugin_commands(files),
      plugin_check_command: PLUGIN_CHECK_COMMAND,
      managed_settings: JSON.pretty_generate(managed_settings(files)),
      login_command: "solidtrace login --ingest-url #{ingest_url} #{console_url}",
      status_command: STATUS_COMMAND,
    }
  end

  def release?(version)
    RELEASE_TAG.match?(version.to_s)
  end

  # The folder of the release's files.
  def release_files(version)
    release?(version) ? "#{RELEASES}/download/#{version}" : "#{RELEASES}/latest/download"
  end

  # Download the marketplace file, add it to Claude Code, and install the plugin.
  def plugin_commands(files)
    [
      "mkdir -p #{File.dirname(MARKETPLACE_FILE)}",
      "curl -fsSL -o #{MARKETPLACE_FILE} #{files}/marketplace.json",
      "claude plugin marketplace add #{MARKETPLACE_FILE}",
      "claude plugin install #{PLUGIN}"
    ]
  end

  # Claude Code's managed settings: the marketplace with a url source (a git
  # source would clone the whole repository on each machine), and the
  # plugin force-enabled, so a person cannot turn it off. In settings, a url
  # source loads (Claude Code 2.1.323): the first start of Claude Code adds
  # the marketplace and gets the plugin, and the plugin runs from the next
  # start.
  def managed_settings(files)
    {
      extraKnownMarketplaces: { MARKETPLACE => { source: { source: "url", url: "#{files}/marketplace.json" } } },
      enabledPlugins: { PLUGIN => true },
    }
  end
end
