<script setup lang="ts">
// Animated number — counts up to its value with an ease-out so figures feel
// alive as they land, instead of snapping into place. Currency- and
// separator-aware: it only animates the numeric part of a string and preserves
// any prefix/suffix ("$", " KES", "%"). Honours prefers-reduced-motion and
// always settles on the exact original string. Shared primitive — reuse it
// anywhere a headline figure appears.
import { ref, watch, onBeforeUnmount } from 'vue'

const props = withDefaults(defineProps<{ value: string | number; duration?: number }>(), {
  duration: 950,
})

const text = ref('')
const reduce =
  typeof window !== 'undefined' && !!window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
let raf = 0

interface Parsed {
  prefix: string
  suffix: string
  value: number
  decimals: number
  grouped: boolean
}

function parse(s: string): Parsed | null {
  const m = s.match(/-?\d[\d,]*\.?\d*/)
  if (!m || m.index === undefined) return null
  const numStr = m[0]
  return {
    prefix: s.slice(0, m.index),
    suffix: s.slice(m.index + numStr.length),
    value: Number(numStr.replace(/,/g, '')),
    decimals: (numStr.split('.')[1] || '').length,
    grouped: numStr.includes(','),
  }
}

function fmt(p: Parsed, v: number): string {
  const body = p.grouped
    ? v.toLocaleString(undefined, { minimumFractionDigits: p.decimals, maximumFractionDigits: p.decimals })
    : v.toFixed(p.decimals)
  return p.prefix + body + p.suffix
}

function animate(to: string) {
  cancelAnimationFrame(raf)
  const p = parse(to)
  if (!p || reduce) {
    text.value = to
    return
  }
  const prev = parse(text.value)
  const from = prev ? prev.value : 0
  const start = performance.now()
  const ease = (t: number) => 1 - Math.pow(1 - t, 3) // easeOutCubic
  const tick = (now: number) => {
    const t = Math.min(1, (now - start) / props.duration)
    text.value = fmt(p, from + (p.value - from) * ease(t))
    if (t < 1) raf = requestAnimationFrame(tick)
    else text.value = to // settle on the exact original formatting
  }
  raf = requestAnimationFrame(tick)
}

watch(() => props.value, (v) => animate(String(v)), { immediate: true })
onBeforeUnmount(() => cancelAnimationFrame(raf))
</script>

<template>
  <span>{{ text }}</span>
</template>
