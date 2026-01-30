
interface StacktraceFrame {
  filename: string
  lineno: number
  function: string
  context_line: string
  pre_context: string[]
  post_context: string[]
  in_app?: boolean
}

interface StacktraceProps {
  frames: StacktraceFrame[]
}

export default function Stacktrace({ frames }: StacktraceProps) {
  if (!frames || frames.length === 0) return null

  return (
    <div className="border rounded-md divide-y overflow-hidden">
      {frames.map((frame, index) => (
        <div key={index} className="bg-background text-sm">
          <div className="px-4 py-2 bg-muted/30 flex items-center justify-between font-mono text-xs">
            <div className="truncate flex-1 mr-4">
              <span className="font-semibold">{frame.filename}</span>
              <span className="text-muted-foreground"> in </span>
              <span className="text-primary">{frame.function}</span>
            </div>
            <div className="text-muted-foreground">
              Line {frame.lineno}
            </div>
          </div>
          <div className="bg-muted overflow-x-auto p-4 font-mono text-xs leading-relaxed text-foreground">
            {frame.pre_context?.map((line, i) => (
              <div key={`pre-${i}`} className="flex">
                <span className="w-8 text-muted-foreground select-none text-right pr-4 opacity-50">
                  {frame.lineno - (frame.pre_context.length - i)}
                </span>
                <pre className="opacity-70 whitespace-pre">{line}</pre>
              </div>
            ))}

            <div className="flex bg-yellow-500/10 -mx-4 px-4 py-0.5 border-y border-yellow-500/20">
              <span className="w-8 text-yellow-600 dark:text-yellow-500 select-none text-right pr-4 font-bold">
                {frame.lineno}
              </span>
              <pre className="font-semibold text-foreground whitespace-pre">{frame.context_line}</pre>
            </div>

            {frame.post_context?.map((line, i) => (
              <div key={`post-${i}`} className="flex">
                <span className="w-8 text-muted-foreground select-none text-right pr-4 opacity-50">
                  {frame.lineno + i + 1}
                </span>
                <pre className="opacity-70 whitespace-pre">{line}</pre>
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  )
}
