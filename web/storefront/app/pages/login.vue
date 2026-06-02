<script setup lang="ts">
import Card from 'primevue/card'
import InputText from 'primevue/inputtext'
import Password from 'primevue/password'
import Button from 'primevue/button'
import Message from 'primevue/message'

const { login } = useAuth()
const route = useRoute()
const router = useRouter()

const email = ref('')
const password = ref('')
const loading = ref(false)
const error = ref('')

useSeoMeta({ title: 'Sign in — Teggo Store' })

async function submit() {
  error.value = ''
  loading.value = true
  try {
    await login(email.value, password.value)
    await router.push((route.query.redirect as string) || '/cart')
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Login failed'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="wrap">
    <Card class="card">
      <template #title>Sign in</template>
      <template #subtitle>Access your contract pricing, carts, quotes, and orders.</template>
      <template #content>
        <form class="form" @submit.prevent="submit">
          <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>
          <div class="field">
            <label for="email">Email</label>
            <InputText id="email" v-model="email" autocomplete="username" fluid />
          </div>
          <div class="field">
            <label for="pwd">Password</label>
            <Password id="pwd" v-model="password" :feedback="false" toggleMask autocomplete="current-password" fluid />
          </div>
          <Button type="submit" label="Sign in" :loading="loading" fluid />
        </form>
      </template>
    </Card>
  </div>
</template>

<style scoped>
.wrap { display: flex; justify-content: center; padding: 3rem 1rem; }
.card { width: 100%; max-width: 400px; }
.form { display: flex; flex-direction: column; gap: 1rem; }
.field { display: flex; flex-direction: column; gap: 0.35rem; }
.field label { font-size: 0.85rem; font-weight: 600; }
</style>
