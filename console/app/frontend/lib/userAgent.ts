/** A browser's user agent in short, from the browser and OS, as "Chrome on macOS". */
export function shortUserAgent(userAgent: string): string {
  const browser =
    /Edg\//.test(userAgent) ? 'Edge'
      : /OPR\//.test(userAgent) ? 'Opera'
        : /Firefox\/|FxiOS/.test(userAgent) ? 'Firefox'
          : /Chrome\/|CriOS/.test(userAgent) ? 'Chrome'
            : /Safari\//.test(userAgent) ? 'Safari'
              : 'Browser'
  const os =
    /iPhone|iPad|iPod/.test(userAgent) ? 'iOS'
      : /Android/.test(userAgent) ? 'Android'
        : /Mac OS X|Macintosh/.test(userAgent) ? 'macOS'
          : /Windows/.test(userAgent) ? 'Windows'
            : /CrOS/.test(userAgent) ? 'ChromeOS'
              : /Linux/.test(userAgent) ? 'Linux'
                : null
  return os ? `${browser} on ${os}` : browser
}
