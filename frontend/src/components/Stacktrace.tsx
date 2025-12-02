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
    <div className="bg-card border rounded-md overflow-hidden">
      <div className="bg-muted/30 px-4 py-2 border-b text-sm font-medium flex justify-between items-center">
        <span className="font-mono text-destructive">{mainException.type}</span>
        <span className="text-muted-foreground">{mainException.value}</span>
      </div>
      <div className="divide-y">
        {reversedFrames.map((frame, index) => (
          <div
            key={index}
            className="p-3 hover:bg-muted/30 transition-colors text-sm"
          >
            <div className="flex justify-between items-start mb-1">
              <div className="font-mono text-muted-foreground">
                {frame.filename || frame.abs_path || 'unknown'}
              </div>
              {frame.lineno && (
                <div className="font-mono text-xs text-muted-foreground">
                  L{frame.lineno}{frame.colno ? `:${frame.colno}` : ''}
                </div>
              )}
            </div>

            {frame.function && (
              <div className="font-mono text-sm mb-2">
                <span className="text-muted-foreground">in</span>{' '}
                <span className="font-semibold text-primary">{frame.function}</span>
              </div>
            )}

            {/* Code context */}
            {frame.context_line && (
              <div className="bg-muted rounded p-2 font-mono text-xs space-y-0.5 overflow-x-auto">
                {frame.pre_context?.slice(-2).map((line, i) => (
                  <div key={`pre-${i}`} className="text-muted-foreground opacity-60">
                    {line}
                  </div>
                ))}
                <div className="bg-destructive/10 text-destructive font-semibold -mx-2 px-2 py-0.5">
                  {frame.context_line}
                </div>
                {frame.post_context?.slice(0, 2).map((line, i) => (
                  <div key={`post-${i}`} className="text-muted-foreground opacity-60">
                    {line}
                  </div>
                ))}
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
