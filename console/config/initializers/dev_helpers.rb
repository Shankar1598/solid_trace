# frozen_string_literal: true

if Rails.env.development?
  def byebug
    debugger
  end

  def disable_pager!
    IRB.conf[:USE_PAGER] = false
  end

  def trigger_test_events!
    (Issue.last.to_s && Project.last.to_s)
    Sentry.capture_message("test message")
    Sentry.capture_exception(StandardError.new("test exception"))
    begin
      1 / 0
    rescue ZeroDivisionError => exception
      Sentry.capture_exception(exception)
    end
  end
end

if Rails.env.production?
  def debugger
    Rails.logger.warn("Debugger called in production")
    Sentry.capture_message("Debugger called in production")
    nil
  end
end

Object.class_eval do
  def ps
    puts self
  end

  def dj
    to_json.ps
  end

  def djp
    to_json(pretty: true).ps
  end
end

def rescue_dj(&block)
  begin
    yield
  rescue Exception => e
    e.dj
  end
end
