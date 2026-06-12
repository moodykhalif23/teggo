<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import Card from 'primevue/card'
import Button from 'primevue/button'
import ProgressSpinner from 'primevue/progressspinner'
import { api, errMessage } from '@/lib/client'

const route = useRoute()
const state = ref<'verifying' | 'ok' | 'error'>('verifying')
const message = ref('')

onMounted(async () => {
  const token = (route.query.token as string) ?? ''
  if (!token) {
    state.value = 'error'
    message.value = 'This verification link is missing its token.'
    return
  }
  const { data, error } = await api.POST('/signup/verify', { body: { token } })
  if (error || !data) {
    state.value = 'error'
    message.value = errMessage(error, 'This verification link is invalid or has expired.')
    return
  }
  state.value = 'ok'
  message.value = data.message ?? 'Organization verified.'
})
</script>

<template>
  <div class="verify-wrap">
    <Card class="verify-card">
      <template #content>
        <div class="body">
          <template v-if="state === 'verifying'">
            <ProgressSpinner style="width: 42px; height: 42px" strokeWidth="4" />
            <p>Verifying your email…</p>
          </template>
          <template v-else-if="state === 'ok'">
            <i class="pi pi-check-circle icon ok" />
            <h3>You're all set</h3>
            <p>{{ message }}</p>
            <RouterLink to="/login"><Button label="Sign in" /></RouterLink>
          </template>
          <template v-else>
            <i class="pi pi-times-circle icon err" />
            <h3>Verification failed</h3>
            <p>{{ message }}</p>
            <RouterLink to="/signup"><Button label="Start over" text /></RouterLink>
          </template>
        </div>
      </template>
    </Card>
  </div>
</template>

<style scoped>
.verify-wrap {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 100vh;
  padding: 1rem;
}
.verify-card {
  width: 100%;
  max-width: 400px;
}
.body {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 0.6rem;
  text-align: center;
  padding: 0.75rem 0;
}
.icon {
  font-size: 2.2rem;
}
.icon.ok {
  color: var(--p-primary-color, #16a34a);
}
.icon.err {
  color: var(--p-red-500, #ef4444);
}
.body h3,
.body p {
  margin: 0;
}
</style>
