// The extra Redaction patterns of Settings → Instance, typed one on each
// line. EventStore checks each pattern at save (InstanceSetting), so here
// the page only splits the text.

/**
 * The patterns of the typed text, one on each line, with no empty lines.
 * A pattern keeps its spaces, as a space in a pattern matches a space.
 */
export function parsePatterns(text: string): string[] {
  return text.split(/\r?\n/).filter(line => line.trim() !== '')
}
