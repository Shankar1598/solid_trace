# frozen_string_literal: true

require "test_helper"

# Console un-minifies the frames of the frontend's errors with its build's
# source maps before it sends them to its Sentry. The fixture bundle is
# test/fixtures/files/browser_source_maps/app/frontend/crash.ts, built with
# esbuild as its first lines say.
class BrowserSourceMapsTest < ActiveSupport::TestCase
  ROOT = Rails.root.join("test/fixtures/files/browser_source_maps")
  BUNDLE = ROOT.join("public/vite/assets/crash-test.js").read
  URL = "https://console.example/vite/assets/crash-test.js"

  setup { BrowserSourceMaps.reset }
  teardown { BrowserSourceMaps.reset }

  test "an event's frames in the build's bundle get the source's file, line, column and function" do
    event = rewritten_event(frame_at("new Error", "n"), frame_at("new TypeError", "h"))
    thrown, shouted = event.dig("exception", "values", 0, "stacktrace", "frames")

    assert_equal({ "filename" => "app/frontend/crash.ts", "abs_path" => URL, "lineno" => 7, "colno" => 11,
                   "function" => "renderChart", "raw_function" => "n", "in_app" => true,
                   "context_line" => "throw new Error('the chart has too many rows')" }, thrown)
    assert_equal [ "app/frontend/crash.ts", 14, "shout" ], shouted.values_at("filename", "lineno", "function")
  end

  test "the event item's length is its new payload's, and the other items stay as they were" do
    report = %({"type":"client_report","length":2}\n{}\n)
    payload = JSON.generate(event_with(frame_at("new Error")))
    body = %({"event_id":"#{"a" * 32}"}\n{"type":"event","length":#{payload.bytesize}}\n#{payload}\n#{report}).b

    lines = rewrite(body).split("\n", -1)

    assert_equal %({"event_id":"#{"a" * 32}"}), lines[0]
    assert_equal lines[2].bytesize, JSON.parse(lines[1])["length"]
    assert_equal "renderChart", JSON.parse(lines[2]).dig("exception", "values", 0, "stacktrace", "frames", 0, "function")
    assert_equal report, "#{lines[3..].join("\n")}"
  end

  test "an envelope with no frame of the build's bundles is sent as it came" do
    dev = frame_at("new Error").merge("filename" => "http://localhost:3036/vite-dev/@fs/app/frontend/crash.ts?v=1",
                                      "abs_path" => "http://localhost:3036/vite-dev/@fs/app/frontend/crash.ts?v=1")
    unknown = frame_at("new Error").merge("filename" => URL.sub("crash-test", "gone"), "abs_path" => URL.sub("crash-test", "gone"))
    body = envelope(event_with(dev, unknown))

    assert_same body, rewrite(body)
    assert_equal "not an envelope", rewrite("not an envelope")
  end

  test "a position the map has no segment for stays as it was" do
    far = frame_at("new Error").merge("lineno" => 99)
    body = envelope(event_with(far))

    assert_same body, rewrite(body)
  end

  test "a frame's function is the one around it, also inside its parameters or an anonymous callback" do
    source = <<~TSX
      const width = (value: number) => value * 2

      export default function DashboardsShow({ dashboard, projects }: Props) {
        useEffect(() => {
          if (dashboard.id > width(1)) {
            refresh(dashboard.id)
          }
        }, [dashboard])
        const onSave = async (chart: Chart): Promise<void> => {
          save(chart)
        }
        return <Card onClick={() => { open(projects) }} />
      }
    TSX
    name = ->(needle) do
      line = source.lines.index { |l| l.include?(needle) }
      BrowserSourceMaps::EnclosingFunction.name(source, line, source.lines[line].index(needle))
    end

    assert_equal "DashboardsShow", name.("dashboard, projects")
    assert_equal "DashboardsShow", name.("refresh(")
    assert_equal "onSave", name.("save(chart)")
    assert_equal "DashboardsShow", name.("open(projects)")
    assert_nil name.("value * 2")
  end

  private

  def rewrite(body)
    BrowserSourceMaps.rewrite_envelope(body, public_dir: ROOT.join("public"), output_dir: "vite")
  end

  def rewritten_event(*frames)
    payload = rewrite(envelope(event_with(*frames))).split("\n")[2]
    JSON.parse(payload)
  end

  # A frame as the browser SDK gives one, at needle in the minified bundle,
  # with lines and columns from 1, in the minified function.
  def frame_at(needle, function = nil)
    index = BUNDLE.index(needle) or raise "#{needle} is not in the bundle"
    line = BUNDLE[0...index].count("\n") + 1
    column = index - (BUNDLE.rindex("\n", index) || -1)
    { "filename" => URL, "abs_path" => URL, "lineno" => line, "colno" => column, "function" => function, "in_app" => true }.compact
  end

  def event_with(*frames)
    { "event_id" => "b" * 32, "platform" => "javascript",
      "exception" => { "values" => [ { "type" => "Error", "value" => "boom", "stacktrace" => { "frames" => frames } } ] } }
  end

  def envelope(event)
    %({"event_id":"#{event["event_id"]}"}\n{"type":"event"}\n#{JSON.generate(event)}\n).b
  end
end
