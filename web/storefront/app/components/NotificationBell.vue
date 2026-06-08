<script setup lang="ts">
import Popover from 'primevue/popover'
import type { SNotification } from '~/composables/useNotifications'

const router = useRouter()
const { items, unread, markRead, markAllRead, start, stop } = useNotifications()

const op = ref()
const badge = computed(() => (unread.value > 99 ? '99+' : String(unread.value)))

onMounted(() => start())
onBeforeUnmount(() => stop())

function toggle(e: Event) {
  op.value?.toggle(e)
}

function onItem(n: SNotification) {
  markRead(n.id)
  op.value?.hide()
  if (n.link) router.push(n.link)
}

function relTime(iso: string): string {
  const s = Math.max(0, Math.floor((Date.now() - new Date(iso).getTime()) / 1000))
  if (s < 45) return 'now'
  if (s < 3600) return `${Math.floor(s / 60)}m`
  if (s < 86400) return `${Math.floor(s / 3600)}h`
  if (s < 604800) return `${Math.floor(s / 86400)}d`
  return new Date(iso).toLocaleDateString()
}
</script>

<template>
  <span class="bell-wrap">
    <button type="button" class="bell" aria-label="Notifications" @click="toggle">
      <i class="pi pi-bell" />
      <span v-if="unread > 0" class="badge">{{ badge }}</span>
    </button>

    <Popover ref="op" class="snotif-pop">
      <div class="snotif">
        <header class="head">
          <span class="title">Notifications</span>
          <button v-if="unread > 0" type="button" class="link" @click="markAllRead()">Mark all read</button>
        </header>

        <ul v-if="items.length" class="list">
          <li v-for="n in items" :key="n.id" class="item" :class="{ unread: !n.read }" @click="onItem(n)">
            <span class="sev" :data-sev="n.severity" />
            <div class="content">
              <div class="row1">
                <span class="t">{{ n.title }}</span>
                <span class="time">{{ relTime(n.created_at) }}</span>
              </div>
              <div v-if="n.body" class="b">{{ n.body }}</div>
            </div>
          </li>
        </ul>

        <div v-else class="empty">
          <i class="pi pi-check-circle" />
          <span>You're all caught up</span>
        </div>
      </div>
    </Popover>
  </span>
</template>

<style scoped>
.bell-wrap {
  display: inline-flex;
}
.bell {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 2.5rem;
  height: 2.5rem;
  border: 1px solid var(--p-surface-200, #e2e8f0);
  border-radius: 6px;
  background: var(--p-surface-0, #fff);
  color: var(--p-text-color, #334155);
  cursor: pointer;
}
.bell:hover {
  background: var(--p-surface-50, #f8fafc);
}
.bell .pi {
  font-size: 1.15rem;
}
.badge {
  position: absolute;
  top: -6px;
  right: -6px;
  min-width: 18px;
  height: 18px;
  padding: 0 4px;
  border-radius: 9px;
  background: #ef4444;
  color: #fff;
  font-size: 0.68rem;
  font-weight: 700;
  line-height: 18px;
  text-align: center;
}
.snotif {
  width: 22rem;
  max-width: 90vw;
}
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0.25rem 0.25rem 0.6rem;
  border-bottom: 1px solid var(--p-surface-200, #e2e8f0);
}
.title {
  font-weight: 700;
}
.link {
  background: none;
  border: none;
  color: var(--p-primary-color, #16a34a);
  font-size: 0.8rem;
  cursor: pointer;
  padding: 0;
}
.list {
  list-style: none;
  margin: 0;
  padding: 0;
  max-height: 24rem;
  overflow-y: auto;
}
.item {
  display: flex;
  gap: 0.6rem;
  padding: 0.65rem 0.4rem;
  border-bottom: 1px solid var(--p-surface-100, #f1f5f9);
  cursor: pointer;
}
.item:hover {
  background: var(--p-surface-50, #f8fafc);
}
.item.unread {
  background: var(--p-primary-50, #f0fdf4);
}
.sev {
  flex: 0 0 8px;
  width: 8px;
  height: 8px;
  margin-top: 0.35rem;
  border-radius: 50%;
  background: var(--p-surface-300, #cbd5e1);
}
.sev[data-sev='success'] { background: #22c55e; }
.sev[data-sev='warning'] { background: #f59e0b; }
.sev[data-sev='error'] { background: #ef4444; }
.sev[data-sev='info'] { background: #3b82f6; }
.content {
  flex: 1;
  min-width: 0;
}
.row1 {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 0.5rem;
}
.t {
  font-size: 0.88rem;
  font-weight: 600;
  color: var(--p-text-color, #1e293b);
}
.time {
  flex: 0 0 auto;
  font-size: 0.72rem;
  color: var(--p-text-muted-color, #94a3b8);
}
.b {
  margin-top: 0.15rem;
  font-size: 0.8rem;
  color: var(--p-text-muted-color, #64748b);
}
.empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 0.4rem;
  padding: 1.5rem 0.5rem;
  color: var(--p-text-muted-color, #94a3b8);
  font-size: 0.85rem;
}
.empty .pi {
  font-size: 1.4rem;
  color: #22c55e;
}
</style>
