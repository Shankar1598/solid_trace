# frozen_string_literal: true

require "test_helper"

# CaptureSetup takes the files of the instance's release only for a release
# tag. Any other version (a development build, or text that only contains a
# tag) gets the newest release's files and a note, because the tag goes into
# a shell command and into machine settings.
class CaptureSetupTest < ActiveSupport::TestCase
  RELEASES = "https://github.com/Shankar1598/solid_trace/releases"

  setup do
    @organization = create(:organization)
  end

  test "a release tag, with or without a prerelease suffix, gives that release's files and no note" do
    [ "v0.19.0", "v0.20.0-rc.1", "v1.2.3-beta.2-x" ].each do |version|
      page = props(version)

      assert_equal "curl -fsSL #{RELEASES}/download/#{version}/install.sh | sh", page[:install_command], version
      assert_equal "#{RELEASES}/download/#{version}/marketplace.json", page[:plugin_commands].second.split.last, version
      assert_equal "#{RELEASES}/download/#{version}/marketplace.json",
                   JSON.parse(page[:managed_settings]).dig("extraKnownMarketplaces", "solidtrace", "source", "url"), version
      assert_nil page[:version_note], version
    end
  end

  test "a version that is not exactly a release tag gives the newest release's files and a note" do
    [ "dev", "v0.19", "xv0.19.0", "v0.19.0y", "v0.19.0 extra", "v1.2.3;id", "v1.2.3\nid", "v0.19.0-", "" ].each do |version|
      page = props(version)

      assert_equal "curl -fsSL #{RELEASES}/latest/download/install.sh | sh", page[:install_command], version.inspect
      assert_equal "#{RELEASES}/latest/download/marketplace.json", page[:plugin_commands].second.split.last, version.inspect
      assert page[:version_note].present?, version.inspect
    end
  end

  private

  def props(version)
    CaptureSetup.props(organization: @organization, version: version,
                       console_url: "https://solidtrace.example.com", ingest_url: "https://solidtrace.example.com:4443")
  end
end
