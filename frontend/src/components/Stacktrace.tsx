import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';

interface StackFrame {
  filename?: string;
  function?: string;
  lineno?: number;
  colno?: number;
  abs_path?: string;
  context_line?: string;
  pre_context?: string[];
  post_context?: string[];
}

interface Exception {
  type?: string;
  value?: string;
  module?: string;
  stacktrace?: {
    frames?: StackFrame[];
  };
}

interface StacktraceProps {
  exception?: {
    values?: Exception[];
  };
}

export function Stacktrace({ exception }: StacktraceProps) {
  if (!exception?.values || exception.values.length === 0) {
    return null;
  }

  const mainException = exception.values[0];
  const frames = mainException.stacktrace?.frames || [];

  if (frames.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-lg">Exception</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="space-y-2">
            <div className="font-semibold text-destructive">
              {mainException.type}: {mainException.value}
            </div>
          </div>
        </CardContent>
      </Card>
    );
  }

  // Reverse frames to show most recent call first (like traditional stack traces)
  const reversedFrames = [...frames].reverse();

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-lg">
          {mainException.type}: {mainException.value}
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="space-y-3">
          {reversedFrames.map((frame, index) => (
            <div
              key={index}
              className="border-l-2 border-muted-foreground/20 pl-4 py-2 hover:border-primary/50 transition-colors"
            >
              <div className="flex items-baseline gap-2 mb-1">
                <span className="font-mono text-sm text-muted-foreground">
                  {frame.filename || frame.abs_path || 'unknown'}
                </span>
                {frame.lineno && (
                  <span className="text-xs text-muted-foreground">
                    line {frame.lineno}
                    {frame.colno && `:${frame.colno}`}
                  </span>
                )}
              </div>

              {frame.function && (
                <div className="font-mono text-sm mb-2">
                  <span className="text-primary">in</span>{' '}
                  <span className="font-semibold">{frame.function}</span>
                </div>
              )}

              {/* Code context */}
              {frame.context_line && (
                <div className="bg-muted/50 rounded p-2 font-mono text-xs space-y-0.5">
                  {frame.pre_context?.slice(-2).map((line, i) => (
                    <div key={`pre-${i}`} className="text-muted-foreground">
                      {line}
                    </div>
                  ))}
                  <div className="bg-destructive/20 px-1 -mx-1 font-semibold">
                    → {frame.context_line}
                  </div>
                  {frame.post_context?.slice(0, 2).map((line, i) => (
                    <div key={`post-${i}`} className="text-muted-foreground">
                      {line}
                    </div>
                  ))}
                </div>
              )}
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  );
}
