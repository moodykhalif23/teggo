<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import Card from 'primevue/card'
import InputText from 'primevue/inputtext'
import InputNumber from 'primevue/inputnumber'
import Password from 'primevue/password'
import Button from 'primevue/button'
import Message from 'primevue/message'

const auth = useAuthStore()
const router = useRouter()
const route = useRoute()

const email = ref('admin@demo.test')
const password = ref('')
// Only revealed when the API says the email exists in several organizations.
const needOrg = ref(false)
const orgId = ref<number | null>(null)
const loading = ref(false)
const error = ref('')

async function submit() {
  error.value = ''
  loading.value = true
  try {
    await auth.login(email.value, password.value, needOrg.value ? (orgId.value ?? undefined) : undefined)
    const redirect = (route.query.redirect as string) || '/'
    router.push(redirect)
  } catch (e) {
    const msg = e instanceof Error ? e.message : 'Login failed'
    if (msg.includes('multiple organizations')) needOrg.value = true
    error.value = msg
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login-wrap">
    <Card class="login-card">
      <template #title>
        <div class="login-title"><i class="pi pi-bolt" /> Teggo Admin</div>
      </template>
      <template #content>
        <form class="form" @submit.prevent="submit">
          <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>
          <div class="field">
            <label for="email">Email</label>
            <InputText id="email" v-model="email" autocomplete="username" fluid />
          </div>
          <div class="field">
            <label for="password">Password</label>
            <Password
              id="password"
              v-model="password"
              :feedback="false"
              toggleMask
              autocomplete="current-password"
              fluid
            />
          </div>
          <div v-if="needOrg" class="field">
            <label for="org">Organization ID</label>
            <InputNumber id="org" v-model="orgId" :useGrouping="false" fluid />
          </div>
          <Button type="submit" label="Sign in" :loading="loading" fluid />
          <p class="signup-hint">
            New to Teggo?
            <RouterLink to="/signup">Create your organization</RouterLink>
          </p>
        </form>
      </template>
    </Card>
  </div>
</template>

<style scoped>
.login-wrap {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 100vh;
  padding: 1rem;
}
.login-card {
  width: 100%;
  max-width: 380px;
}
.login-title {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.form {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
}
.field label {
  font-size: 0.85rem;
  font-weight: 600;
}
.signup-hint {
  margin: 0;
  text-align: center;
  font-size: 0.85rem;
  color: var(--p-text-muted-color, #64748b);
}
</style>
