<script setup lang="ts">
import Card from 'primevue/card'
import InputText from 'primevue/inputtext'
import Password from 'primevue/password'
import Button from 'primevue/button'
import Message from 'primevue/message'

const route = useRoute()
const router = useRouter()
const client = useClient()
const { acceptInvite } = useAuth()
const inviteToken = route.params.token as string

// SSR-resolve the invite so the page greets the buyer with the company they're
// joining (or a clear error when the link is dead).
const { data: invite, error: inviteError } = await useAsyncData(`invite-${inviteToken}`, async () => {
  const { data, error } = await client.GET('/storefront/invites/{token}', {
    params: { path: { token: inviteToken } },
  })
  if (error || !data) {
    throw createError({
      statusCode: 410,
      statusMessage: (error as { message?: string } | undefined)?.message || 'This invite link is no longer valid.',
    })
  }
  return data
})

useSeoMeta({ title: () => (invite.value ? `Join ${invite.value.company_name} — Teggo Store` : 'Join — Teggo Store') })

const fullName = ref('')
const email = ref('')
const password = ref('')
const loading = ref(false)
const error = ref('')

async function submit() {
  error.value = ''
  if (password.value.length < 8) {
    error.value = 'Password must be at least 8 characters.'
    return
  }
  loading.value = true
  try {
    await acceptInvite(inviteToken, {
      email: email.value,
      full_name: fullName.value,
      password: password.value,
    })
    await router.push('/account')
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Could not create your account'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="wrap">
    <Message v-if="inviteError" severity="error" :closable="false">This invite link is no longer valid.</Message>
    <Card v-else-if="invite" class="card">
      <template #title>Join {{ invite.company_name }}</template>
      <template #subtitle>
        You've been invited to order on behalf of {{ invite.company_name }} as a {{ invite.role }}.
        Create your account to get started.
      </template>
      <template #content>
        <form class="form" @submit.prevent="submit">
          <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>
          <div class="field">
            <label for="name">Full name</label>
            <InputText id="name" v-model="fullName" autocomplete="name" fluid />
          </div>
          <div class="field">
            <label for="email">Work email</label>
            <InputText id="email" v-model="email" autocomplete="username" fluid />
          </div>
          <div class="field">
            <label for="pwd">Password</label>
            <Password id="pwd" v-model="password" toggleMask autocomplete="new-password" fluid />
          </div>
          <Button type="submit" label="Create account" :loading="loading" fluid />
          <p class="muted">
            Already have an account? <NuxtLink to="/login">Sign in</NuxtLink>
          </p>
        </form>
      </template>
    </Card>
  </div>
</template>

<style scoped>
.wrap { display: flex; justify-content: center; padding: 3rem 1rem; }
.card { width: 100%; max-width: 420px; }
.form { display: flex; flex-direction: column; gap: 1rem; }
.field { display: flex; flex-direction: column; gap: 0.35rem; }
.field label { font-size: 0.85rem; font-weight: 600; }
.muted { color: var(--p-text-muted-color, #64748b); font-size: 0.88rem; text-align: center; margin: 0; }
</style>
