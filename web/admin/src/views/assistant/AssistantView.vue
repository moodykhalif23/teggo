<script setup lang="ts">
import { nextTick, ref } from 'vue'
import Button from 'primevue/button'
import Textarea from 'primevue/textarea'
import { api, errMessage } from '@/lib/client'
import type { components } from '@teggo/api/schema'
import PageHeader from '@/components/PageHeader.vue'

type Turn = components['schemas']['AssistantTurn']

const messages = ref<Turn[]>([])
const input = ref('')
const busy = ref(false)
const scroller = ref<HTMLElement | null>(null)

const suggestions = [
  { icon: 'pi pi-chart-line', text: 'Show me the receivables aging' },
  { icon: 'pi pi-heart', text: 'Which accounts are at risk of churn?' },
  { icon: 'pi pi-search', text: 'Look up a recent order' },
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

function clearChat() {
  messages.value = []
}

async function scroll() {
  await nextTick()
  if (scroller.value) scroller.value.scrollTop = scroller.value.scrollHeight
}
</script>

<template>
  <div class="page assistant">
    <PageHeader title="Assistant" meta="AI">
      <template #actions>
        <Button
          v-if="messages.length"
          icon="pi pi-eraser"
          label="Clear chat"
          size="small"
          severity="secondary"
          text
          @click="clearChat"
        />
      </template>
    </PageHeader>

    <p class="disclaimer">
      <i class="pi pi-shield" />
      Ask about orders, receivables and accounts. The assistant only runs permission-gated tools
      on your behalf — it never invents data.
    </p>

    <div ref="scroller" class="thread">
      <!-- Welcome / empty state -->
      <div v-if="!messages.length" class="welcome">
        <span class="welcome-badge"><i class="pi pi-sparkles" /></span>
        <h2>How can I help?</h2>
        <p class="muted">Pick a starting point, or ask anything about your workspace.</p>
        <div class="chips">
          <button v-for="s in suggestions" :key="s.text" type="button" class="chip" @click="send(s.text)">
            <i :class="s.icon" />
            <span>{{ s.text }}</span>
            <i class="pi pi-arrow-right chip-go" />
          </button>
        </div>
      </div>

      <!-- Conversation — clean single-column flow -->
      <div v-for="(m, i) in messages" :key="i" class="msg" :class="m.role">
        <span class="role">{{ m.role === 'user' ? 'You' : 'Assistant' }}</span>
        <div class="bubble">{{ m.text }}</div>
      </div>

      <!-- Typing indicator -->
      <div v-if="busy" class="msg assistant">
        <span class="role">Assistant</span>
        <div class="bubble typing">
          <span class="dots"><span></span><span></span><span></span></span>
        </div>
      </div>
    </div>

    <form class="composer" @submit.prevent="send()">
      <Textarea
        v-model="input"
        rows="1"
        autoResize
        placeholder="Ask the assistant…"
        @keydown.enter.exact.prevent="send()"
      />
      <Button type="submit" icon="pi pi-send" rounded :loading="busy" :disabled="!input.trim()" aria-label="Send" />
    </form>
    <p class="hint">Press <kbd>Enter</kbd> to send · <kbd>Shift</kbd>+<kbd>Enter</kbd> for a new line</p>
  </div>
</template>

<style scoped>
.assistant {
  display: flex;
  flex-direction: column;
  height: calc(100dvh - 5rem);
}
.disclaimer {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin: 0 0 0.5rem;
  font-size: 0.82rem;
  color: var(--p-text-muted-color, #64748b);
}
.disclaimer .pi {
  font-size: 0.9rem;
  opacity: 0.7;
}
.muted {
  color: var(--p-text-muted-color, #64748b);
}

/* Thread */
.thread {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 1rem 0.25rem;
  display: flex;
  flex-direction: column;
  gap: 0.85rem;
}

/* Welcome */
.welcome {
  margin: auto;
  text-align: center;
  max-width: 30rem;
  padding: 1rem;
}
.welcome-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 48px;
  height: 48px;
  border-radius: 12px;
  background: color-mix(in srgb, var(--p-primary-color, #16a34a) 14%, transparent);
  color: var(--p-primary-color, #16a34a);
  font-size: 1.4rem;
  margin-bottom: 0.75rem;
}
.welcome h2 {
  margin: 0 0 0.25rem;
  font-size: 1.2rem;
  font-weight: 700;
}
.chips {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  margin-top: 1.25rem;
}
.chip {
  display: flex;
  align-items: center;
  gap: 0.65rem;
  width: 100%;
  padding: 0.7rem 0.9rem;
  border: 1px solid var(--teggo-border, #e2e8f0);
  border-radius: 10px;
  background: var(--teggo-surface, #fff);
  cursor: pointer;
  font: inherit;
  text-align: left;
  color: var(--p-text-color, #334155);
  transition: border-color 0.15s, background 0.15s;
}
.chip:hover {
  border-color: var(--p-primary-color, #16a34a);
  background: var(--p-surface-50, #f8fafc);
}
.chip > span {
  flex: 1;
}
.chip > .pi:first-child {
  color: var(--p-primary-color, #16a34a);
}
.chip-go {
  color: var(--p-text-muted-color, #94a3b8);
  font-size: 0.8rem;
}

/* Messages — clean single-column flow (document style). Your turns sit in a
   subtle bordered card; the assistant's reply is plain flowing text. */
.msg {
  width: 100%;
  max-width: 46rem;
  margin: 0 auto;
}
.role {
  display: block;
  font-size: 0.7rem;
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--p-text-muted-color, #94a3b8);
  margin-bottom: 0.3rem;
}
.bubble {
  white-space: pre-wrap;
  line-height: 1.6;
  font-size: 0.92rem;
  color: var(--p-text-color, #1e293b);
}
.msg.user .bubble {
  background: var(--p-surface-50, #f8fafc);
  border: 1px solid var(--teggo-border, #e2e8f0);
  border-radius: 10px;
  padding: 0.75rem 1rem;
}
.msg.assistant .bubble {
  padding: 0.1rem 0.1rem 0.4rem;
}

/* Typing dots */
.typing {
  padding: 0.35rem 0.1rem;
}
.dots {
  display: inline-flex;
  gap: 4px;
}
.dots span {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--p-text-muted-color, #94a3b8);
  animation: blink 1.4s infinite both;
}
.dots span:nth-child(2) {
  animation-delay: 0.2s;
}
.dots span:nth-child(3) {
  animation-delay: 0.4s;
}
@keyframes blink {
  0%,
  80%,
  100% {
    opacity: 0.25;
  }
  40% {
    opacity: 1;
  }
}

/* Composer */
.composer {
  display: flex;
  gap: 0.5rem;
  align-items: flex-end;
  padding: 0.75rem;
  border: 1px solid var(--teggo-border, #e2e8f0);
  border-radius: 14px;
  background: var(--teggo-surface, #fff);
  margin: 0.5rem auto 0;
  width: 100%;
  max-width: 46rem;
}
.composer :deep(textarea) {
  flex: 1;
  resize: none;
  border: none;
  box-shadow: none;
  padding: 0.35rem 0.4rem;
  background: transparent;
  max-height: 9rem;
}
.composer :deep(textarea:focus) {
  outline: none;
}
.hint {
  margin: 0.4rem 0 0;
  text-align: center;
  font-size: 0.72rem;
  color: var(--p-text-muted-color, #94a3b8);
}
.hint kbd {
  font-family: inherit;
  font-size: 0.7rem;
  padding: 0.05rem 0.3rem;
  border: 1px solid var(--teggo-border, #e2e8f0);
  border-radius: 4px;
  background: var(--p-surface-50, #f8fafc);
}
</style>
