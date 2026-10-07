import { afterEach, describe, expect, it, vi } from 'vitest'
import { createApp, nextTick } from 'vue'
import MemberLevelPrices from './MemberLevelPrices.vue'

const api = vi.hoisted(() => ({
  getProducts: vi.fn(), getProduct: vi.fn(), getMemberLevelPrices: vi.fn(),
  batchUpsertMemberLevelPrices: vi.fn(), deleteMemberLevelPrice: vi.fn(),
}))
vi.mock('@/api/admin', () => ({ adminAPI: api }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/utils/format', () => ({ getLocalizedText: (value: Record<string, string>) => value['zh-CN'] }))
vi.mock('@/utils/notify', () => ({ notifyError: vi.fn(), notifySuccess: vi.fn() }))
vi.mock('@/utils/confirm', () => ({ confirmAction: async () => true }))
vi.mock('@/components/ui/dialog', () => {
  const slot = { template: '<div><slot /></div>' }
  return { Dialog: slot, DialogHeader: slot, DialogScrollContent: slot, DialogTitle: slot }
})

let cleanup: (() => void) | undefined
afterEach(() => { cleanup?.(); vi.clearAllMocks() })
const settle = async () => { await new Promise(resolve => setTimeout(resolve, 0)); await nextTick() }

async function mount() {
  api.getProducts.mockResolvedValue({ data: { data: [{ id: 10, title: { 'zh-CN': 'Product' } }], pagination: { total_page: 1 } } })
  api.getProduct.mockResolvedValue({ data: { data: { id: 10, title: { 'zh-CN': 'Product' }, skus: [{ id: 11, sku_code: 'ONE', spec_values: {} }] } } })
  api.getMemberLevelPrices.mockResolvedValue({ data: { data: [
    { id: 20, member_level_id: 2, product_id: 10, sku_id: 0, price_amount: 158 },
    { id: 21, member_level_id: 3, product_id: 10, sku_id: 0, price_amount: 88 },
  ] } })
  api.batchUpsertMemberLevelPrices.mockResolvedValue({})
  api.deleteMemberLevelPrice.mockResolvedValue({})
  const root = document.createElement('div')
  document.body.append(root)
  const app = createApp(MemberLevelPrices, { level: { id: 2, name: { 'zh-CN': 'VIP' } } })
  app.mount(root)
  cleanup = () => { app.unmount(); root.remove() }
  await settle()
  root.querySelector<HTMLButtonElement>('button[aria-pressed]')!.click()
  await settle()
  return root
}

describe('member level pricing entry', () => {
  it('loads only the current level rules and saves a SKU override', async () => {
    const root = await mount()
    expect(root.textContent).toContain('158.00')
    expect(root.textContent).not.toContain('88.00')
    const select = root.querySelector('select')!
    select.value = '11'
    select.dispatchEvent(new Event('change'))
    await nextTick()
    const input = root.querySelector<HTMLInputElement>('input[type="number"]')!
    input.value = '150.50'
    input.dispatchEvent(new Event('input'))
    await nextTick()
    root.querySelectorAll('form')[1]!.dispatchEvent(new Event('submit', { cancelable: true }))
    await settle()
    expect(api.batchUpsertMemberLevelPrices).toHaveBeenCalledWith({ prices: [{ member_level_id: 2, product_id: 10, sku_id: 11, price_amount: 150.5 }] })
  })

  it('rejects invalid prices and deletes the selected level rule', async () => {
    const root = await mount()
    const input = root.querySelector<HTMLInputElement>('input[type="number"]')!
    input.value = '-1'
    input.dispatchEvent(new Event('input'))
    await nextTick()
    root.querySelectorAll('form')[1]!.dispatchEvent(new Event('submit', { cancelable: true }))
    await settle()
    expect(api.batchUpsertMemberLevelPrices).not.toHaveBeenCalled()
    root.querySelector<HTMLButtonElement>('button[aria-label="admin.common.delete"]')!.click()
    await settle()
    expect(api.deleteMemberLevelPrice).toHaveBeenCalledWith(20)
    expect(root.textContent).not.toContain('158.00')
  })
})
