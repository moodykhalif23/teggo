// Shapes returned by the Go storefront API. These will be replaced by the
// generated TypeScript client from the OpenAPI contract (Pack 2 §5).
export interface Product {
  public_id: string
  sku: string
  name: string
  slug: string
  description?: string
  status: 'draft' | 'active' | 'disabled'
  unit: string
  attributes?: Record<string, unknown>
}

export interface ProductList {
  items: Product[]
  page: number
  total?: number
}
