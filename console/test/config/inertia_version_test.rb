# frozen_string_literal: true

require "test_helper"

# Each Inertia response carries the version of the frontend's files. Puma
# answers requests in several threads, and ViteRuby reads that version in a
# Dir.chdir block, which raises when another thread changes the directory at
# the same time: the page then answers 500 (seen with parallel Playwright
# workers).
class InertiaVersionTest < ActiveSupport::TestCase
  test "the version answers in many threads at once" do
    # ViteRuby reads the files again once its last read is a second old, so 2 s gives the threads a few reads to meet in.
    stop = Time.now + 2
    threads = Array.new(8) do
      Thread.new do
        versions = []
        versions << InertiaRails.configuration.version while Time.now < stop
        versions.uniq
      end
    end

    assert_equal 1, threads.flat_map(&:value).uniq.size
  end
end
