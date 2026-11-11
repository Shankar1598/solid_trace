# frozen_string_literal: true

require "test_helper"

# Console's URL and ingest's URL: the instance setting, else the env var, else
# the request's address (Console) or Console's host at port 4000 (ingest).
class PublicUrlsTest < ActiveSupport::TestCase
  REQUEST = "http://10.0.0.5:3000"

  test "with nothing saved or set, Console's URL is the request's address, and ingest is its host at port 4000" do
    assert_equal REQUEST, PublicUrls.console_url(REQUEST, env: {})
    assert_equal "http://10.0.0.5:4000", PublicUrls.ingest_url(REQUEST, env: {})
  end

  test "without a request, a job or mailer has no URL unless one is saved or set" do
    assert_nil PublicUrls.console_url(env: {})
    assert_nil PublicUrls.ingest_url(env: {})
    assert_nil PublicUrls.console_url_options(env: {})
  end

  test "the env vars come before the request, without a trailing slash" do
    env = { "CONSOLE_URL" => "https://console.example.com/", "PUBLIC_INGEST_URL" => "https://ingest.example.com:4443/" }

    assert_equal "https://console.example.com", PublicUrls.console_url(REQUEST, env: env)
    assert_equal "https://ingest.example.com:4443", PublicUrls.ingest_url(REQUEST, env: env)
  end

  test "a blank env var is unset" do
    env = { "CONSOLE_URL" => " ", "PUBLIC_INGEST_URL" => "" }

    assert_equal REQUEST, PublicUrls.console_url(REQUEST, env: env)
    assert_equal "http://10.0.0.5:4000", PublicUrls.ingest_url(REQUEST, env: env)
  end

  test "ingest without its own URL follows Console's, at port 4000" do
    assert_equal "https://console.example.com:4000",
      PublicUrls.ingest_url(REQUEST, env: { "CONSOLE_URL" => "https://console.example.com" })

    InstanceSetting.current.update!(console_url: "https://solid-trace.example.ts.net")
    assert_equal "https://solid-trace.example.ts.net:4000", PublicUrls.ingest_url(REQUEST, env: {})
  end

  test "the saved settings come before the env vars and the request" do
    InstanceSetting.current.update!(console_url: "https://solid-trace.example.ts.net",
      ingest_url: "https://35.214.179.92:4443/sentry")
    env = { "CONSOLE_URL" => "https://console.example.com", "PUBLIC_INGEST_URL" => "https://ingest.example.com" }

    assert_equal "https://solid-trace.example.ts.net", PublicUrls.console_url(REQUEST, env: env)
    assert_equal "https://35.214.179.92:4443/sentry", PublicUrls.ingest_url(REQUEST, env: env)
    assert_equal "https://solid-trace.example.ts.net", PublicUrls.console_url(env: env)
  end

  test "the fallbacks are what a blank setting uses, with where they come from" do
    InstanceSetting.current.update!(console_url: "https://solid-trace.example.ts.net")

    assert_equal({ url: REQUEST, source: "request" }, PublicUrls.console_url_fallback(REQUEST, env: {}))
    assert_equal({ url: "https://console.example.com", source: "CONSOLE_URL" },
      PublicUrls.console_url_fallback(REQUEST, env: { "CONSOLE_URL" => "https://console.example.com" }))
    assert_equal({ url: "https://solid-trace.example.ts.net:4000", source: "console_url" },
      PublicUrls.ingest_url_fallback(REQUEST, env: {}))
    assert_equal({ url: "https://ingest.example.com", source: "PUBLIC_INGEST_URL" },
      PublicUrls.ingest_url_fallback(REQUEST, env: { "PUBLIC_INGEST_URL" => "https://ingest.example.com" }))
  end

  test "url options for links built without a request name Console's host, port and scheme" do
    InstanceSetting.current.update!(console_url: "https://solid-trace.example.ts.net")
    assert_equal({ host: "solid-trace.example.ts.net", port: 443, protocol: "https" }, PublicUrls.console_url_options(env: {}))

    assert_equal({ host: "10.0.0.5", port: 3000, protocol: "http" },
      PublicUrls.console_url_options(settings: InstanceSetting.new, env: { "CONSOLE_URL" => REQUEST }))
  end
end
