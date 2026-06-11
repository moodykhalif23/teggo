// Shared catalogs for the automation flow builder. Trigger events + per-event
// payload fields are what the backend dispatcher emits; action keys are the
// registered actions (internal/automation). The flow canvas and the rule list
// both read from here so they never drift.

export const TRIGGER_EVENTS = ['order.status_changed', 'quote.expired', 'schedule.hourly', 'schedule.daily']

export const FIELDS_BY_EVENT: Record<string, string[]> = {
  'order.status_changed': ['status', 'from', 'to', 'grand_total', 'customer_id', 'order_number'],
  'quote.expired': ['quote_number', 'customer_id'],
}

export const OPS = [
  { label: '= equals', value: 'eq' },
  { label: '≠ not equals', value: 'ne' },
  { label: '> greater than', value: 'gt' },
  { label: '≥ at least', value: 'gte' },
  { label: '< less than', value: 'lt' },
  { label: '≤ at most', value: 'lte' },
]

export interface ActionDef {
  key: string
  label: string
  params: { name: string; label: string; placeholder?: string }[]
}

export const ACTION_CATALOG: ActionDef[] = [
  { key: 'email_customer', label: 'Email the customer', params: [{ name: 'template', label: 'Email template', placeholder: 'order_status_update' }] },
  { key: 'expire_quotes', label: 'Expire stale quotes', params: [] },
  { key: 'mark_overdue', label: 'Mark invoices overdue + dun', params: [] },
  { key: 'quote_followup', label: 'Follow up on expiring quotes', params: [{ name: 'within_days', label: 'Days before expiry', placeholder: '3' }] },
  { key: 'cart_recovery', label: 'Recover abandoned carts', params: [{ name: 'idle_hours', label: 'Idle hours before nudge', placeholder: '24' }] },
]

export function actionDef(key: string): ActionDef | undefined {
  return ACTION_CATALOG.find((a) => a.key === key)
}

export function fieldsFor(triggerEvent: string): string[] {
  return FIELDS_BY_EVENT[triggerEvent] ?? []
}

// Preserve JSON types when serializing condition values: numeric strings ->
// number, true/false -> boolean.
export function coerce(v: string): unknown {
  const t = v.trim()
  if (t === '') return ''
  if (/^-?\d+(\.\d+)?$/.test(t)) return Number(t)
  if (t === 'true') return true
  if (t === 'false') return false
  return t
}

// Local builder types (uid keeps action nodes stable across add/remove).
export interface Cond {
  field: string
  op: string
  value: string
}
export interface Act {
  uid: string
  key: string
  params: Record<string, string>
}

let uidSeq = 0
export function uid(): string {
  uidSeq += 1
  return `a${uidSeq}-${Math.floor(Math.random() * 1e6).toString(36)}`
}
