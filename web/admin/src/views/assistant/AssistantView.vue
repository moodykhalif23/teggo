<script setup lang="ts">
import { nextTick, ref } from 'vue'
import Button from 'primevue/button'
import Textarea from 'primevue/textarea'
import { api, errMessage } from '@/lib/client'
import type { components } from '@teggo/api/schema'

type Turn = components['schemas']['AssistantTurn']

const messages = ref<Turn[]>([])
const input = ref('')
const busy = ref(false)
const scroller = ref<HTMLElement | null>(null)

const suggestions = [
  'Show me the receivables aging',
  'Which accounts are at risk of churn?',
  'Look up order …',
]

async function send(text?: string) {
  const msg = (text ?? input.value).trim()
  if (!msg || busy.value) return
  input.value = ''
  messages.value.push({ role: 'user', text: msg })
  await scroll()
  busy.value = true
  // Send prior turns as history (exclude the just-pushed user message).
  const history = messages.value.slice(0, -1)
  const { data, error } = await api.POST('/admin/assistant', { body: { message: msg, history } })
  busy.value = false
  const reply = data?.text ?? errMessage(error, 'The assistant is unavailable right now.')
  messages.value.push({ role: 'assistant', text: reply })
  await scroll()
}

async function scroll() {
  await nextTick()
  if (scroller.value) scroller.value.scrollTop = scroller.value.scrollHeight
}
</script>

<template>
  <div class="page">
    <p class="muted">Ask about orders, receivables and accounts. The assistant only runs permission-gated tools on your behalf — it never invents data.</p>

    <div ref="scroller" class="thread">
      <div v-if="!messages.length" class="empty">
        <p class="muted">Try one of these:</p>
        <div class="chips">
          <Button v-for="s in suggestions" :key="s" :label="s" size="small" outlined @click="send(s)" />
        </div>
      </div>
      <div v-for="(m, i) in messages" :key="i" class="msg" :class="m.role">
        <div class="bubble">{{ m.text }}</div>
      </div>
      <div v-if="busy" class="msg assistant"><div class="bubble muted">…</div></div>
    </div>

    <form class="composer" @submit.prevent="send()">
      <Textarea v-model="input" rows="1" autoResize placeholder="Ask the assistant…" @keydown.enter.exact.prevent="send()" />
      <Button type="submit" icon="pi pi-send" :loading="busy" :disabled="!input.trim()" />
    </form>
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; height: calc(100vh - 6rem); }
.page h1 { margin: 0; }
.muted { color: var(--p-text-muted-color, #64748b); }
.thread { flex: 1; overflow-y: auto; padding: 1rem 0; display: flex; flex-direction: column; gap: 0.75rem; }
.empty { margin-top: 1rem; }
.chips { display: flex; flex-wrap: wrap; gap: 0.5rem; margin-top: 0.5rem; }
.msg { display: flex; }
.msg.user { justify-content: flex-end; }
.bubble { max-width: 70%; padding: 0.6rem 0.85rem; border-radius: 12px; white-space: pre-wrap; line-height: 1.4; }
.msg.user .bubble { background: #1d4ed8; color: #fff; border-bottom-right-radius: 4px; }
.msg.assistant .bubble { background: var(--p-surface-100, #f1f5f9); border-bottom-left-radius: 4px; }
.composer { display: flex; gap: 0.5rem; align-items: flex-end; padding-top: 0.5rem; border-top: 1px solid var(--p-surface-200, #e2e8f0); }
.composer :deep(textarea) { flex: 1; resize: none; }
</style>
