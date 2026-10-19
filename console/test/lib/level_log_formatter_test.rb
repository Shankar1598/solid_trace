require "test_helper"
require "level_log_formatter"

# Vector reads a production line's Level from its start (docs/adr/0011).
class LevelLogFormatterTest < ActiveSupport::TestCase
  test "a line starts with its Level, then its tags and message" do
    out = StringIO.new
    logger = LevelLogFormatter.logger(out)

    logger.tagged("4f2a-request") { logger.info("Started GET \"/\"") }
    logger.tagged("4f2a-request") { logger.error("Boom") }

    assert_equal "INFO [4f2a-request] Started GET \"/\"\nERROR [4f2a-request] Boom\n", out.string
  end

  test "a line without tags is its Level and message" do
    out = StringIO.new

    LevelLogFormatter.logger(out).warn("Disk nearly full")

    assert_equal "WARN Disk nearly full\n", out.string
  end
end
