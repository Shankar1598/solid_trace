import * as React from "react"
import { Moon, Sun } from "lucide-react"
import { cn } from "@/lib/utils"
import { THEME_CHANGED, isDark, toggleTheme } from "@/lib/theme"

import { Button } from "@/components/ui/button"

export function ModeToggle({ className }: { className?: string }) {
  const [dark, setDark] = React.useState(false)

  // The palette can toggle the theme too.
  React.useEffect(() => {
    const follow = () => setDark(isDark())
    follow()
    window.addEventListener(THEME_CHANGED, follow)
    return () => window.removeEventListener(THEME_CHANGED, follow)
  }, [])

  return (
    <Button variant="ghost" size="icon" onClick={toggleTheme} title="Toggle theme" className={cn("h-8 w-8 text-muted-foreground hover:text-foreground", className)}>
      {dark ? (
        <Moon className="h-4 w-4" />
      ) : (
        <Sun className="h-4 w-4" />
      )}
    </Button>
  )
}
