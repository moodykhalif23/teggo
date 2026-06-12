<script setup lang="ts">
import { reactive, ref } from 'vue'
import Card from 'primevue/card'
import InputText from 'primevue/inputtext'
import Password from 'primevue/password'
import Select from 'primevue/select'
import Button from 'primevue/button'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'

const currencies = ['KES', 'USD', 'EUR', 'GBP', 'TZS', 'UGX']

const form = reactive({
  organization: '',
  full_name: '',
  email: '',
  password: '',
  subdomain: '',
  currency: 'KES',
})
const loading = ref(false)
const error = ref('')
const done = ref(false)
const domain = ref('')

// Mirror the org name into a suggested subdomain until the user edits it.
const subdomainTouched = ref(false)
function suggestSubdomain() {
  if (subdomainTouched.value) return
  form.subdomain = form.organization
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 30)
}

async function submit() {
  error.value = ''
  loading.value = true
  const { data, error: err } = await api.POST('/signup', { body: { ...form } })
  loading.value = false
  if (err || !data) {
    error.value = errMessage(err, 'Could not create the organization')
    return
  }
  domain.value = data.domain ?? ''
  done.value = true
}
</script>

<template>
  <div class="signup-wrap">
    <Card class="signup-card">
      <template #title>
        <div class="signup-title"><i class="pi pi-bolt" /> Create your organization</div>
      </template>
      <template #subtitle>Your team, catalog and storefront — live in minutes.</template>
      <template #content>
        <div v-if="done" class="done">
          <i class="pi pi-envelope done-icon" />
          <h3>Check your email</h3>
          <p>
            We sent a verification link to <strong>{{ form.email }}</strong
            >. Click it to activate <strong>{{ form.organization }}</strong
            >.
          </p>
          <p v-if="domain" class="muted">Your storefront will live at <strong>{{ domain }}</strong></p>
          <RouterLink to="/login"><Button label="Back to sign in" text /></RouterLink>
        </div>

        <form v-else class="form" @submit.prevent="submit">
          <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>
          <div class="field">
            <label for="org">Company name</label>
            <InputText id="org" v-model="form.organization" placeholder="Acme Industrial" fluid @input="suggestSubdomain" />
          </div>
          <div class="field">
            <label for="sub">Store address</label>
            <InputText
              id="sub"
              v-model="form.subdomain"
              placeholder="acme"
              fluid
              @input="subdomainTouched = true"
            />
            <small class="muted">Lowercase letters, digits and hyphens — becomes your storefront subdomain.</small>
          </div>
          <div class="field">
            <label for="name">Your name</label>
            <InputText id="name" v-model="form.full_name" placeholder="Ada Admin" fluid />
          </div>
          <div class="field">
            <label for="email">Work email</label>
            <InputText id="email" v-model="form.email" autocomplete="email" fluid />
          </div>
          <div class="field">
            <label for="password">Password</label>
            <Password id="password" v-model="form.password" toggleMask autocomplete="new-password" fluid />
            <small class="muted">At least 8 characters.</small>
          </div>
          <div class="field">
            <label for="currency">Currency</label>
            <Select id="currency" v-model="form.currency" :options="currencies" fluid />
          </div>
          <Button type="submit" label="Create organization" :loading="loading" fluid />
          <p class="signin-hint">
            Already have an account?
            <RouterLink to="/login">Sign in</RouterLink>
          </p>
        </form>
      </template>
    </Card>
  </div>
</template>

<style scoped>
.signup-wrap {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 100vh;
  padding: 1rem;
}
.signup-card {
  width: 100%;
  max-width: 440px;
}
.signup-title {
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
.muted {
  color: var(--p-text-muted-color, #64748b);
}
.signin-hint {
  margin: 0;
  text-align: center;
  font-size: 0.85rem;
  color: var(--p-text-muted-color, #64748b);
}
.done {
  text-align: center;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  align-items: center;
  padding: 0.5rem 0;
}
.done-icon {
  font-size: 2rem;
  color: var(--p-primary-color, #16a34a);
}
.done h3 {
  margin: 0;
}
.done p {
  margin: 0;
}
</style>
