<script setup lang="ts">
import { ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Password from 'primevue/password'
import Message from 'primevue/message'
import { auth } from '@/lib/auth'

const router = useRouter()
const route = useRoute()

const email = ref('')
const password = ref('')
const orgId = ref(1)
const error = ref('')
const busy = ref(false)

async function submit() {
  busy.value = true
  error.value = ''
  try {
    await auth.login(email.value, password.value, orgId.value)
    const redirect = (route.query.redirect as string) || '/'
    router.push(redirect)
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Login failed'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="wrap">
    <form class="card" @submit.prevent="submit">
      <div class="brand"><i class="pi pi-shop" /> Teggo Vendor Portal</div>
      <p class="sub">Sign in to manage your products, orders and payouts.</p>
      <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>
      <div class="field"><label>Email</label><InputText v-model="email" autofocus /></div>
      <div class="field"><label>Password</label><Password v-model="password" :feedback="false" toggleMask inputClass="w-full" /></div>
      <Button type="submit" label="Sign in" :loading="busy" :disabled="!email || !password" class="submit" />
    </form>
  </div>
</template>

<style scoped>
.wrap { display: grid; place-items: center; height: 100%; }
.card { width: 22rem; background: #fff; border: 1px solid #e2e8f0; border-radius: 12px; padding: 1.75rem; }
.brand { font-weight: 700; font-size: 1.2rem; display: flex; gap: 0.5rem; align-items: center; }
.sub { color: #64748b; font-size: 0.9rem; margin: 0.4rem 0 1.25rem; }
.field { display: flex; flex-direction: column; gap: 0.35rem; margin-bottom: 0.9rem; }
.field label { font-size: 0.82rem; font-weight: 600; }
.field :deep(.p-password), .field :deep(.p-password-input) { width: 100%; }
.submit { width: 100%; margin-top: 0.5rem; }
.mb { margin-bottom: 1rem; }
</style>
