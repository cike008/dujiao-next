import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, nextTick } from 'vue'
import ResellerWithdraws from './ResellerWithdraws.vue'

const api = vi.hoisted(() => ({ getResellerSettlementSettings: vi.fn(), updateResellerSettlementSettings: vi.fn(), getResellerWithdraws: vi.fn() }))
const notices = vi.hoisted(() => ({ notifyError: vi.fn(), notifySuccess: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: api }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('vue-router', () => ({ useRoute: () => ({ query: {} }) }))
vi.mock('@/utils/notify', () => notices)
vi.mock('@/utils/confirm', () => ({ confirmAction: vi.fn() }))
vi.mock('@/components/ComplianceGuardWrapper.vue', () => ({ default: { template: '<div><slot /></div>' } }))
vi.mock('@/components/ListPagination.vue', () => ({ default: { template: '<div />' } }))

let cleanup: (() => void) | undefined
const settle = async () => { await new Promise(resolve => setTimeout(resolve, 0)); await nextTick() }
beforeEach(() => {
  vi.resetAllMocks()
  api.getResellerWithdraws.mockResolvedValue({ data: { data: [] } })
  api.getResellerSettlementSettings.mockResolvedValue({ data: { data: { confirm_days: 7 } } })
  api.updateResellerSettlementSettings.mockResolvedValue({ data: { data: { confirm_days: 0 } } })
})
afterEach(() => cleanup?.())
async function mount() {
  const root = document.createElement('div')
  document.body.append(root)
  const app = createApp(ResellerWithdraws)
  app.mount(root)
  cleanup = () => { app.unmount(); root.remove() }
  await settle()
  return root
}
describe('reseller settlement settings', () => {
  it('loads current days and saves an explicit zero', async () => {
    const root = await mount()
    const input = root.querySelector<HTMLInputElement>('#reseller-confirm-days')!
    expect(input.value).toBe('7')
    input.value = '0'
    input.dispatchEvent(new Event('input'))
    await nextTick()
    root.querySelector('form')!.dispatchEvent(new Event('submit', { cancelable: true }))
    await settle()
    expect(api.updateResellerSettlementSettings).toHaveBeenCalledWith(0)
    expect(notices.notifySuccess).toHaveBeenCalled()
  })
  it('blocks saving when the current policy cannot be loaded', async () => {
    api.getResellerSettlementSettings.mockRejectedValue(new Error('unavailable'))
    const root = await mount()
    expect(root.querySelector<HTMLInputElement>('#reseller-confirm-days')!.disabled).toBe(true)
    root.querySelector('form')!.dispatchEvent(new Event('submit', { cancelable: true }))
    await settle()
    expect(api.updateResellerSettlementSettings).not.toHaveBeenCalled()
  })
})
