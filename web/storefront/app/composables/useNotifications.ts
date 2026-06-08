// Storefront buyer notification feed. Durable history is fetched over HTTP from
// the Go API; real-time arrives via Pusher when the API reports it's configured,
// otherwise unread state is refreshed by polling. State lives in useState so it
// is shared across components and SSR-safe; all network/Pusher work happens
// client-side only. The endpoints aren't in the generated client, so we call
// $fetch with the configured API base + the buyer's bearer cookie.
import Pusher from 'pusher-js'

export interface SNotification {
  id: string
  type: string
  title: string
  body?: string
  link?: string
  severity: 'info' | 'success' | 'warning' | 'error'
  read: boolean
  created_at: string
}

interface Realtime {
  enabled: boolean
  key: string
  cluster: string
  channel: string
}

interface Feed {
  items?: SNotification[]
  unread_count?: number
  realtime?: Realtime
}

const POLL_MS = 30_000
// Singletons across composable calls (a Nuxt app has one of each).
let pusher: Pusher | null = null
let pollTimer: ReturnType<typeof setInterval> | null = null

export function useNotifications() {
  const config = useRuntimeConfig()
  const base = config.public.apiBase
  const token = useCookie<string | null>('teggo_token')

  const items = useState<SNotification[]>('notif:items', () => [])
  const unread = useState<number>('notif:unread', () => 0)
  const realtime = useState<Realtime | null>('notif:realtime', () => null)

  const PREFIX = `${base}/storefront/notifications`
  const headers = (): Record<string, string> =>
    token.value ? { Authorization: `Bearer ${token.value}` } : {}

  async function load() {
    if (!import.meta.client || !token.value) return
    try {
      const d = await $fetch<Feed>(`${PREFIX}?limit=20`, { headers: headers() })
      items.value = d.items ?? []
      unread.value = d.unread_count ?? 0
      realtime.value = d.realtime ?? null
      connectRealtime()
    } catch {
      /* offline — recovered by the next poll/realtime event */
    }
  }

  async function markRead(id: string) {
    const n = items.value.find((x) => x.id === id)
    if (n && !n.read) {
      n.read = true
      unread.value = Math.max(0, unread.value - 1)
    }
    try {
      const d = await $fetch<{ unread_count?: number }>(`${PREFIX}/${id}/read`, { method: 'POST', headers: headers() })
      unread.value = d.unread_count ?? unread.value
    } catch {
      /* optimistic */
    }
  }

  async function markAllRead() {
    items.value.forEach((n) => (n.read = true))
    unread.value = 0
    try {
      await $fetch(`${PREFIX}/read-all`, { method: 'POST', headers: headers() })
    } catch {
      /* optimistic */
    }
  }

  function connectRealtime() {
    if (!import.meta.client) return
    if (realtime.value?.enabled && !pusher) {
      pusher = new Pusher(realtime.value.key, {
        cluster: realtime.value.cluster,
        channelAuthorization: {
          endpoint: `${PREFIX}/pusher-auth`,
          transport: 'ajax',
          headers: headers(),
        },
      })
      const ch = pusher.subscribe(realtime.value.channel)
      ch.bind('notification.created', (n: SNotification) => {
        if (items.value.some((x) => x.id === n.id)) return
        items.value = [n, ...items.value].slice(0, 50)
        if (!n.read) unread.value += 1
      })
    } else if (!realtime.value?.enabled) {
      if (pollTimer === null) pollTimer = setInterval(load, POLL_MS)
    }
  }

  function start() {
    load()
  }
  function stop() {
    if (pusher) {
      pusher.disconnect()
      pusher = null
    }
    if (pollTimer !== null) {
      clearInterval(pollTimer)
      pollTimer = null
    }
    items.value = []
    unread.value = 0
    realtime.value = null
  }

  return { items, unread, realtime, load, markRead, markAllRead, start, stop }
}
