<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import { useConfirm } from 'primevue/useconfirm'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import type { components } from '@teggo/api/schema'
import PageHeader from '@/components/PageHeader.vue'
import { useAuthStore } from '@/stores/auth'

type Org = components['schemas']['PlatformOrganization']

const toast = useToast()
const confirm = useConfirm()
const auth = useAuthStore()
const orgs = ref<Org[]>([])
const loading = ref(false)
const error = ref('')

const severities: Record<string, string> = {
  active: 'success',
  trial: 'info',
  pending: 'warn',
  suspended: 'danger',
}

const planCodes = ref<string[]>([])

async function load() {
  loading.value = true
  error.value = ''
  const { data, error: err } = await api.GET('/admin/platform/organizations')
  loading.value = false
  if (err || !data) {
    error.value = errMessage(err, 'Failed to load organizations')
    return
  }
  orgs.value = data.items ?? []
  if (!planCodes.value.length) {
    const { data: plans } = await api.GET('/admin/platform/plans')
    planCodes.value = (plans?.items ?? []).map((p) => p.code ?? '').filter(Boolean)
  }
}

async function setPlan(org: Org, code: string) {
  const { error: err } = await api.POST('/admin/platform/organizations/{id}/plan', {
    params: { path: { id: org.id! } },
    body: { plan_code: code },
  })
  if (err) {
    toast.add({ severity: 'error', summary: errMessage(err, 'Plan change failed'), life: 4000 })
    return
  }
  toast.add({ severity: 'success', summary: `${org.name} moved to ${code}`, life: 2500 })
  load()
}

async function setStatus(org: Org, status: 'active' | 'suspended') {
  const { error: err } = await api.POST('/admin/platform/organizations/{id}/status', {
    params: { path: { id: org.id! } },
    body: { status },
  })
  if (err) {
    toast.add({ severity: 'error', summary: errMessage(err, 'Status change failed'), life: 4000 })
    return
  }
  toast.add({ severity: 'success', summary: `${org.name} is now ${status}`, life: 2500 })
  load()
}

function suspendOrg(org: Org) {
  confirm.require({
    header: 'Suspend organization',
    message: `Suspend ${org.name}? Every sign-in and API call for this tenant will be refused until it is reactivated.`,
    icon: 'pi pi-exclamation-triangle',
    acceptProps: { label: 'Suspend', severity: 'danger' },
    rejectProps: { label: 'Cancel', severity: 'secondary', outlined: true },
    accept: () => setStatus(org, 'suspended'),
  })
}

onMounted(load)
</script>

<template>
  <div class="page">
    <PageHeader title="Platform" subtitle="Every tenant organization on this deployment — lifecycle, size, status.">
      <Button icon="pi pi-refresh" text :loading="loading" @click="load" />
    </PageHeader>
    <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>

    <DataTable :value="orgs" :loading="loading" dataKey="id" stripedRows>
      <Column field="id" header="ID" style="width: 4rem" />
      <Column field="name" header="Organization" />
      <Column header="Status" style="width: 9rem">
        <template #body="{ data: o }">
          <Tag :value="o.status" :severity="severities[o.status] ?? 'secondary'" />
        </template>
      </Column>
      <Column header="Plan" style="width: 9rem">
        <template #body="{ data: o }">
          <Select
            v-if="o.id !== auth.orgId && planCodes.length"
            :modelValue="o.plan_code || null"
            :options="planCodes"
            size="small"
            placeholder="—"
            @update:modelValue="(code: string) => setPlan(o, code)"
          />
          <span v-else>{{ o.plan_code || '—' }}</span>
        </template>
      </Column>
      <Column field="user_count" header="Users" style="width: 6rem" />
      <Column field="website_count" header="Websites" style="width: 7rem" />
      <Column header="Created" style="width: 11rem">
        <template #body="{ data: o }">{{ o.created_at ? new Date(o.created_at).toLocaleDateString() : '—' }}</template>
      </Column>
      <Column header="" style="width: 11rem">
        <template #body="{ data: o }">
          <span v-if="o.id === auth.orgId" class="muted">this organization</span>
          <template v-else>
            <Button
              v-if="o.status === 'suspended'"
              label="Reactivate"
              size="small"
              outlined
              @click="setStatus(o, 'active')"
            />
            <Button
              v-else-if="o.status !== 'pending'"
              label="Suspend"
              size="small"
              severity="danger"
              outlined
              @click="suspendOrg(o)"
            />
            <span v-else class="muted">awaiting verification</span>
          </template>
        </template>
      </Column>
    </DataTable>
  </div>
</template>
