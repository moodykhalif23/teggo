import { ref, computed } from 'vue'
import { api } from '@/lib/client'

export function useCustomerOptions() {
  const customers = ref<{ id: number; name: string }[]>([])
  const customersLoaded = ref(false)
  async function loadCustomers() {
    if (customersLoaded.value) return
    const { data } = await api.GET('/admin/customers', {
      params: { query: { page: 1, page_size: 200 } },
    })
    customers.value = (data?.items ?? []).map((x) => ({ id: x.id, name: x.name }))
    customersLoaded.value = true
  }
  return { customers, customersLoaded, loadCustomers }
}

export function useProductOptions() {
  const products = ref<{ id: number; sku: string; name: string }[]>([])
  const productsLoaded = ref(false)
  // "SKU — Name" label for the dropdown; binds the numeric id.
  const productOptions = computed(() =>
    products.value.map((p) => ({ id: p.id, label: `${p.sku} — ${p.name}` })),
  )
  async function loadProducts() {
    if (productsLoaded.value) return
    const { data } = await api.GET('/admin/products', {
      params: { query: { page: 1, page_size: 200 } },
    })
    products.value = (data?.items ?? []).map((x) => ({ id: x.id, sku: x.sku, name: x.name }))
    productsLoaded.value = true
  }
  return { products, productOptions, productsLoaded, loadProducts }
}
