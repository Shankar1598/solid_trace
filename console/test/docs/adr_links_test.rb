require "test_helper"

# A link to an ADR in the docs gives the ADR's number in its text. The number
# must be the number of the file that the link goes to, and a file in this
# repo must exist, so that a reader who looks up the ADR by its number finds it.
class AdrLinksTest < ActiveSupport::TestCase
  DOCS = Rails.root.join("../docs")
  LINK = /\[ADR (\d{4})\]\(([^)]*?(\d{4})-[^)]*\.md)\)/

  test "each ADR link in the docs names the number of the ADR it goes to" do
    wrong = []
    Dir.glob(DOCS.join("**/*.md")).sort.each do |path|
      File.read(path).scan(LINK) do |text_number, target, file_number|
        name = Pathname(path).relative_path_from(DOCS).to_s
        wrong << "#{name}: ADR #{text_number} goes to #{target}" if text_number != file_number
        next if target.include?("://")
        wrong << "#{name}: #{target} does not exist" unless File.exist?(File.expand_path(target, File.dirname(path)))
      end
    end
    assert_empty wrong
  end
end
