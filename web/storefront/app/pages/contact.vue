<script setup lang="ts">
import Card from 'primevue/card'
import InputText from 'primevue/inputtext'
import Textarea from 'primevue/textarea'
import Button from 'primevue/button'
import Message from 'primevue/message'

useSeoMeta({
  title: 'Contact us — Teggo Store',
  description: 'Get in touch with our sales team.',
})

const client = useClient()

const form = reactive({ contact_name: '', company_name: '', email: '', phone: '', notes: '' })
const busy = ref(false)
const error = ref('')
const sent = ref(false)

async function submit() {
  error.value = ''
  if (!form.contact_name.trim()) {
    error.value = 'Please tell us your name.'
    return
  }
  if (!form.email.trim() && !form.phone.trim()) {
    error.value = 'Please give us an email or phone number so we can reply.'
    return
  }
  busy.value = true
  const { error: err } = await client.POST('/storefront/leads', {
    body: {
      contact_name: form.contact_name.trim(),
      company_name: form.company_name.trim() || null,
      email: form.email.trim() || null,
      phone: form.phone.trim() || null,
      notes: form.notes.trim() || null,
    },
  })
  busy.value = false
  if (err) {
    error.value = 'Could not send your enquiry. Please try again.'
    return
  }
  sent.value = true
}
</script>

<template>
  <section class="wrap">
    <h1 class="title">Contact our sales team</h1>
    <p class="muted">Tell us what you need and we'll get back to you with pricing and availability.</p>

    <Card>
      <template #content>
        <Message v-if="sent" severity="success" :closable="false">
          Thanks — your enquiry has been received. Our team will be in touch shortly.
        </Message>

        <form v-else @submit.prevent="submit">
          <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>
          <div class="field"><label>Your name</label><InputText v-model="form.contact_name" :disabled="busy" /></div>
          <div class="field"><label>Company <span class="muted">(optional)</span></label><InputText v-model="form.company_name" :disabled="busy" /></div>
          <div class="row">
            <div class="field"><label>Email</label><InputText v-model="form.email" type="email" :disabled="busy" /></div>
            <div class="field"><label>Phone</label><InputText v-model="form.phone" :disabled="busy" /></div>
          </div>
          <div class="field"><label>How can we help?</label><Textarea v-model="form.notes" rows="4" :disabled="busy" /></div>
          <Button type="submit" label="Send enquiry" icon="pi pi-send" :loading="busy" />
        </form>
      </template>
    </Card>
  </section>
</template>

<style scoped>
.wrap { max-width: 640px; }
.title { margin: 0 0 0.4rem; }
.muted { color: var(--p-text-muted-color, #64748b); font-weight: 400; }
.mb { margin-bottom: 1rem; }
.field { display: flex; flex-direction: column; gap: 0.35rem; margin-bottom: 0.9rem; }
.field label { font-size: 0.85rem; font-weight: 600; }
.row { display: grid; grid-template-columns: 1fr 1fr; gap: 0.75rem; }
</style>
