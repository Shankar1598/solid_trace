// A Project's Log file order, as its settings page shows and edits it: null
// for the default, or the Fields each hour's Log file is sorted by before
// time ([] is time only). Console's Project model checks an order on save.

/** The built-in Fields a Log file order can name. */
export const SORTED_BUILT_INS = ['service', 'environment', 'level']

/** The order in words, such as "Default (service, file)". */
export function orderSummary(order: string[] | null, defaultOrder: string[]): string {
  if (order === null) return `Default (${defaultOrder.join(', ')})`
  if (order.length === 0) return 'Time only'
  return `${order.join(', ')}, then time`
}

/** The Fields to offer for the order: the built-in ones, then the string Attributes, less those it names. */
export function offeredFields(order: string[], attributes: string[]): string[] {
  const names = [...new Set([...SORTED_BUILT_INS, ...attributes])]
  return names.filter(name => !order.includes(name))
}

/** The order with a typed name added at its end. */
export function addField(order: string[], typed: string): string[] {
  const name = typed.trim()
  return name === '' ? order : [...order, name]
}
