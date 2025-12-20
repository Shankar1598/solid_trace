import { cn } from "@/lib/utils"

interface SparklineProps extends React.HTMLAttributes<HTMLDivElement> {
  data?: number[]
}

export function Sparkline({ className, data = [0, 0, 0, 0, 0, 0, 0, 0, 0, 0] }: SparklineProps) {
  // Generate random data if none provided (for visual effect in prototype)
  const values = data.length > 0 ? data : Array.from({ length: 24 }, () => Math.floor(Math.random() * 50))
  const max = Math.max(...values, 1)

  return (
    <div className={cn("flex items-end gap-[1px] h-8 w-24 opacity-60", className)}>
      {values.map((v, i) => (
        <div
          key={i}
          className="bg-foreground flex-1 rounded-sm"
          style={{ height: `${(v / max) * 100}%` }}
        />
      ))}
    </div>
  )
}
