<script setup lang="ts">
// Shared Lottie motion primitive — the app's vocabulary for soulful, lightweight
// animation (empty states, success moments, loading, onboarding). Give it an
// `animationData` object (import a .json from src/assets/lottie) or a `src` URL.
//
// Craft defaults: it only plays while on screen (IntersectionObserver) so motion
// is never wasted off-view, and it fully respects prefers-reduced-motion —
// reduced users see a calm static last frame, never a loop. If the animation has
// no data yet, the default slot renders instead, so callers always have a clean
// fallback and the UI is never blank.
import { onMounted, onBeforeUnmount, ref, watch } from 'vue'
import lottie, { type AnimationItem } from 'lottie-web'

const props = withDefaults(
  defineProps<{
    /** Parsed Lottie JSON (import animationData from a .json asset). */
    animationData?: object | null
    /** Or a URL/path to a .json animation. */
    src?: string
    loop?: boolean
    autoplay?: boolean
    /** Playback speed multiplier. */
    speed?: number
  }>(),
  { animationData: null, loop: true, autoplay: true, speed: 1 },
)

const host = ref<HTMLElement | null>(null)
const ready = ref(false)
let anim: AnimationItem | null = null
let io: IntersectionObserver | null = null

const reduce =
  typeof window !== 'undefined' && !!window.matchMedia?.('(prefers-reduced-motion: reduce)').matches

function build() {
  if (!host.value || (!props.animationData && !props.src)) return
  destroy()
  anim = lottie.loadAnimation({
    container: host.value,
    renderer: 'svg',
    loop: reduce ? false : props.loop,
    autoplay: false, // gated on visibility below
    ...(props.animationData
      ? { animationData: JSON.parse(JSON.stringify(props.animationData)) }
      : { path: props.src }),
  })
  anim.addEventListener('DOMLoaded', () => {
    ready.value = true
    anim?.setSpeed(props.speed)
    if (reduce) {
      // Settle on a representative static frame rather than animating.
      anim?.goToAndStop(anim.totalFrames - 1, true)
    } else if (props.autoplay) {
      observe()
    }
  })
  anim.addEventListener('error', () => destroy())
}

function observe() {
  if (!host.value || typeof IntersectionObserver === 'undefined') {
    anim?.play()
    return
  }
  io = new IntersectionObserver(
    (entries) => {
      for (const e of entries) {
        if (e.isIntersecting) anim?.play()
        else anim?.pause()
      }
    },
    { threshold: 0.1 },
  )
  io.observe(host.value)
}

function destroy() {
  io?.disconnect()
  io = null
  ready.value = false
  anim?.destroy()
  anim = null
}

onMounted(build)
onBeforeUnmount(destroy)
watch(() => [props.animationData, props.src], build)
watch(() => props.speed, (s) => anim?.setSpeed(s))
</script>

<template>
  <div class="lottie" :class="{ 'lottie--ready': ready }">
    <div ref="host" class="lottie__canvas" aria-hidden="true" />
    <!-- Fallback shown until (or unless) the animation loads. -->
    <div v-if="!ready" class="lottie__fallback"><slot /></div>
  </div>
</template>

<style scoped>
.lottie {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
}
.lottie__canvas {
  width: 100%;
  height: 100%;
}
.lottie__fallback {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
}
</style>
