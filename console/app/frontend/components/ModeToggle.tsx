import * as React from "react"
import { Moon, Sun } from "lucide-react"
import { cn } from "@/lib/utils"

import { Button } from "@/components/ui/button"

export function ModeToggle({ className }: { className?: string }) {
  const [theme, setTheme] = React.useState<"light" | "dark" | "system">("light")

  React.useEffect(() => {
    const isDark =
      localStorage.theme === "dark" ||
      (!("theme" in localStorage) &&
        window.matchMedia("(prefers-color-scheme: dark)").matches)
    setTheme(isDark ? "dark" : "light")
  }, [])

  const toggleTheme = () => {
    const newTheme = theme === "light" ? "dark" : "light"
    setTheme(newTheme)
    if (newTheme === "dark") {
      document.documentElement.classList.add("dark")
      localStorage.theme = "dark"
    } else {
      document.documentElement.classList.remove("dark")
      localStorage.theme = "light"
    }
  }

  return (
    <Button variant="ghost" size="icon" onClick={toggleTheme} title="Toggle theme" className={cn("h-8 w-8 text-muted-foreground hover:text-foreground", className)}>
      {theme === "light" ? (
        <Sun className="h-4 w-4" />
      ) : (
        <Moon className="h-4 w-4" />
      )}
    </Button>
  )
}
