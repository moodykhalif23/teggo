<script setup lang="ts">
import Card from 'primevue/card'
import Tag from 'primevue/tag'
import Button from 'primevue/button'
import Message from 'primevue/message'

definePageMeta({ middleware: 'auth' })
useSeoMeta({ title: 'Account settings — Teggo Store' })

const client = useClient()
const router = useRouter()

const { data: profile, error } = await useAsyncData('account-company', async () => {
  const { data, error } = await client.GET('/storefront/account/company')
  if (error || !data) throw createError({ statusCode: 502, statusMessage: 'Could not load account' })
  return data
})

const isAdmin = computed(() => profile.value?.me.role === 'admin')
const canApprove = computed(() => profile.value?.me.role === 'admin' || profile.value?.me.role === 'approver')
</script>

<template>
  <section class="wrap">
    <h1 class="title">Account settings</h1>
    <Message v-if="error" severity="error" :closable="false">Could not load your account.</Message>

    <template v-if="profile">
      <div class="grid">
        <Card>
          <template #title>Company</template>
          <template #content>
            <dl class="dl">
              <dt>Name</dt><dd>{{ profile.company.name }}</dd>
              <dt>Tax ID</dt><dd>{{ profile.company.tax_id || '—' }}</dd>
              <dt>Payment terms</dt><dd>{{ profile.company.payment_terms_days ?? 0 }} days</dd>
              <dt>Credit limit</dt><dd>{{ profile.company.credit_limit ?? '—' }}</dd>
            </dl>
          </template>
        </Card>

        <Card>
          <template #title>Your profile</template>
          <template #content>
            <dl class="dl">
              <dt>Name</dt><dd>{{ profile.me.full_name }}</dd>
              <dt>Email</dt><dd>{{ profile.me.email }}</dd>
              <dt>Role</dt><dd><Tag :value="profile.me.role" /></dd>
              <dt>Spending limit</dt><dd>{{ profile.me.spending_limit || 'Unlimited' }}</dd>
            </dl>
          </template>
        </Card>
      </div>

      <div class="admin">
        <Button v-if="isAdmin" label="Manage company users" icon="pi pi-users" @click="router.push('/account/users')" />
        <Button v-if="canApprove" label="Review order approvals" icon="pi pi-check-square" outlined @click="router.push('/account/approvals')" />
      </div>
    </template>
  </section>
</template>

<style scoped>
.wrap { max-width: 820px; }
.title { margin: 0 0 1.25rem; }
.grid { display: grid; grid-template-columns: 1fr 1fr; gap: 1.25rem; }
.dl { display: grid; grid-template-columns: 9rem 1fr; gap: 0.5rem 1rem; margin: 0; }
.dl dt { color: var(--p-text-muted-color, #64748b); font-size: 0.85rem; }
.dl dd { margin: 0; }
.admin { margin-top: 1.5rem; display: flex; gap: 0.75rem; flex-wrap: wrap; }
</style>
