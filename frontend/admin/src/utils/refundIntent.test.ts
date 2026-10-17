import { afterEach, describe, expect, it, vi } from 'vitest'
import { webcrypto } from 'node:crypto'
import { getRefundIntent } from './refundIntent'

afterEach(() => { vi.unstubAllGlobals(); sessionStorage.clear() })
describe('manual refund intent', () => {
  it('reuses failed requests, then rotates only after confirmed success', async () => {
    vi.stubGlobal('crypto', webcrypto)
    const payload = { amount: '5.00', remark: 'private refund reason' }
    const first = await getRefundIntent(1, payload)
    const retry = await getRefundIntent(1, payload)
    expect(retry.requestId).toBe(first.requestId)
    expect(JSON.stringify(sessionStorage)).not.toContain(payload.remark)
    first.complete()
    const next = await getRefundIntent(1, payload)
    expect(next.requestId).not.toBe(first.requestId)
    next.complete()
  })
  it('does not reuse an identity for a different order or amount', async () => {
    vi.stubGlobal('crypto', webcrypto)
    const a = await getRefundIntent(1, { amount: '5.00' })
    const b = await getRefundIntent(2, { amount: '5.00' })
    const c = await getRefundIntent(1, { amount: '6.00' })
    expect(new Set([a.requestId, b.requestId, c.requestId]).size).toBe(3)
    a.complete(); b.complete(); c.complete()
  })
})
