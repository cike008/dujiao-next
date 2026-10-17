type RefundIntentPayload = { amount: string; remark?: string; payment_fee_refunded?: boolean }
const memory = new Map<string, string>()

// Keep the same identity across a lost response and a page reload, without
// storing the refund amount, reason, or customer details in session storage.
export async function getRefundIntent(orderId: number, payload: RefundIntentPayload) {
  const bytes = new TextEncoder().encode(JSON.stringify([orderId, payload.amount, payload.remark || '', !!payload.payment_fee_refunded]))
  const digest = await crypto.subtle.digest('SHA-256', bytes)
  const fingerprint = Array.from(new Uint8Array(digest), b => b.toString(16).padStart(2, '0')).join('')
  const key = `manual-refund-intent:${fingerprint}`
  let requestId = memory.get(key)
  try { requestId = sessionStorage.getItem(key) || requestId } catch { /* In-memory fallback for restricted storage. */ }
  if (!requestId) requestId = crypto.randomUUID()
  memory.set(key, requestId)
  try { sessionStorage.setItem(key, requestId) } catch { /* Keep identity in this tab. */ }
  return {
    requestId,
    complete: () => {
      memory.delete(key)
      try { sessionStorage.removeItem(key) } catch { /* Storage may be unavailable. */ }
    },
  }
}
