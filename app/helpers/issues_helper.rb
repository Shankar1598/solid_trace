module IssuesHelper
  def level_color(level)
    case level
    when 50 then 'bg-red-500' # Fatal
    when 40 then 'bg-orange-500' # Error
    when 30 then 'bg-yellow-500' # Warning
    when 20 then 'bg-blue-500' # Info
    when 10 then 'bg-gray-400' # Debug
    else 'bg-gray-300'
    end
  end

  def level_label(level)
    case level
    when 50 then 'fatal'
    when 40 then 'error'
    when 30 then 'warning'
    when 20 then 'info'
    when 10 then 'debug'
    else 'unknown'
    end
  end
end
