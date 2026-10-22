// A Project's Log file order, as its settings page shows and edits it: null
// for the default, or the Fields each hour's Log file is sorted by before
// time ([] is time only). Console's Project model checks an order on save.

/** How many Fields a Log file order can name, as the Project model allows. */
const MAX_FIELDS = 3

/** The order in words, such as "Default (service, file)". */
export function orderSummary(order: string[] | null, defaultOrder: string[]): string {
  if (order === null) return `Default (${defaultOrder.join(', ')})`
  if (order.length === 0) return 'Time only'
  return `${order.join(', ')}, then time`
}

/** The Fields to offer for the order: the built-in ones it can name, then the string Attributes, less those it names. */
export function offeredFields(order: string[], builtIns: string[], attributes: string[]): string[] {
  const names = [...new Set([...builtIns, ...attributes])]
  return names.filter(name => !order.includes(name))
}

/** Why a typed name can't be added to the order, or null when it can. */
export function whyNotAdded(order: string[], typed: string): string | null {
  const name = typed.trim()
  if (order.includes(name)) return `${name} is in the order already`
  if (order.length >= MAX_FIELDS) return `A Log file order has at most ${MAX_FIELDS} Fields`
  return null
}

/** The order with a typed name added at its end. */
export function addField(order: string[], typed: string): string[] {
  const name = typed.trim()
  return name === '' ? order : [...order, name]
}
