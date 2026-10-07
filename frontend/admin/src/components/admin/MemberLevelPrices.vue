<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Search, Trash2, ChevronLeft, ChevronRight } from 'lucide-vue-next'
import { adminAPI } from '@/api/admin'
import type { AdminMemberLevel, AdminMemberLevelPrice, AdminProduct } from '@/api/types'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Dialog, DialogHeader, DialogScrollContent, DialogTitle } from '@/components/ui/dialog'
import { getLocalizedText } from '@/utils/format'
import { notifyError, notifySuccess } from '@/utils/notify'
import { confirmAction } from '@/utils/confirm'

const props = defineProps<{ level: AdminMemberLevel }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const key = 'admin.memberLevels.levelPrices.'
const query = ref('')
const products = ref<AdminProduct[]>([])
const page = ref(1)
const totalPages = ref(1)
const loading = ref(false)
const saving = ref(false)
const product = ref<AdminProduct | null>(null)
const prices = ref<AdminMemberLevelPrice[]>([])
const skuID = ref(0)
const amount = ref('')
const error = ref('')
const ownPrices = computed(() => prices.value.filter(p => p.member_level_id === props.level.id))
const skuLabel = (id: number) => {
  if (!id) return t(key + 'productScope')
  const sku = product.value?.skus?.find(s => s.id === id)
  return sku ? [sku.sku_code, ...Object.values(sku.spec_values || {})].filter(Boolean).join(' / ') : `SKU #${id}`
}
const editScope = () => {
  const existing = ownPrices.value.find(p => p.sku_id === Number(skuID.value))
  amount.value = existing ? String(existing.price_amount) : ''
}
const search = async (nextPage = 1) => {
  loading.value = true
  error.value = ''
  try {
    const res = await adminAPI.getProducts({ page: nextPage, page_size: 20, search: query.value.trim() })
    products.value = res.data.data || []
    page.value = nextPage
    totalPages.value = res.data.pagination?.total_page || 1
  } catch {
    error.value = t(key + 'loadFailed')
    products.value = []
  } finally { loading.value = false }
}
const selectProduct = async (id: number) => {
  loading.value = true
  product.value = null
  prices.value = []
  skuID.value = 0
  amount.value = ''
  error.value = ''
  try {
    const [detail, pricing] = await Promise.all([adminAPI.getProduct(id), adminAPI.getMemberLevelPrices(id)])
    product.value = detail.data.data
    prices.value = pricing.data.data || []
    editScope()
  } catch { error.value = t(key + 'loadFailed') }
  finally { loading.value = false }
}
const save = async () => {
  if (!product.value || saving.value || loading.value) return
  if (!/^\d+(\.\d{1,2})?$/.test(String(amount.value).trim()) || Number(amount.value) <= 0) {
    error.value = t(key + 'invalidPrice')
    return
  }
  saving.value = true
  error.value = ''
  const id = product.value.id
  try {
    await adminAPI.batchUpsertMemberLevelPrices({ prices: [{
      member_level_id: props.level.id, product_id: id,
      sku_id: Number(skuID.value), price_amount: Number(amount.value),
    }] })
    notifySuccess(t(key + 'saveSuccess'))
    await selectProduct(id)
  } catch { error.value = t(key + 'saveFailed') }
  finally { saving.value = false }
}
const remove = async (price: AdminMemberLevelPrice) => {
  if (!await confirmAction({ description: t(key + 'deleteConfirm'), confirmText: t('admin.common.delete'), variant: 'destructive' })) return
  saving.value = true
  try {
    await adminAPI.deleteMemberLevelPrice(price.id)
    prices.value = prices.value.filter(p => p.id !== price.id)
    editScope()
    notifySuccess(t(key + 'deleteSuccess'))
  } catch { notifyError(t(key + 'deleteFailed')) }
  finally { saving.value = false }
}
onMounted(() => search())
</script>

<template>
  <Dialog :open="true" @update:open="value => { if (!value && !saving) emit('close') }">
    <DialogScrollContent class="w-[calc(100vw-1rem)] max-w-3xl p-4 sm:p-6" @interact-outside="(e: Event) => e.preventDefault()">
      <DialogHeader><DialogTitle>{{ t(key + 'title') }} · {{ getLocalizedText(level.name) }}</DialogTitle></DialogHeader>
      <form class="flex gap-2" @submit.prevent="search()">
        <Input v-model="query" :placeholder="t(key + 'search')" :aria-label="t(key + 'search')" :disabled="loading || saving" />
        <Button type="submit" variant="outline" :disabled="loading || saving" :title="t(key + 'search')" :aria-label="t(key + 'search')"><Search class="h-4 w-4" /></Button>
      </form>
      <div class="max-h-44 overflow-y-auto divide-y divide-border border-y border-border" :aria-busy="loading">
        <button v-for="item in products" :key="item.id" type="button" :disabled="loading || saving" :aria-pressed="product?.id === item.id" class="block w-full px-3 py-2 text-left text-sm hover:bg-muted disabled:opacity-50" :class="product?.id === item.id ? 'bg-primary/10 text-primary' : ''" @click="selectProduct(item.id)">
          #{{ item.id }} · {{ getLocalizedText(item.title) }}
        </button>
        <p v-if="!loading && !products.length" class="py-4 text-sm text-muted-foreground">{{ t('admin.memberLevels.empty') }}</p>
      </div>
      <div class="flex items-center justify-end gap-3 text-sm">
        <Button variant="outline" size="sm" :disabled="loading || saving || page <= 1" :aria-label="t(key + 'previous')" @click="search(page - 1)"><ChevronLeft class="h-4 w-4" /></Button>
        <span>{{ page }} / {{ totalPages }}</span>
        <Button variant="outline" size="sm" :disabled="loading || saving || page >= totalPages" :aria-label="t(key + 'next')" @click="search(page + 1)"><ChevronRight class="h-4 w-4" /></Button>
      </div>
      <form v-if="product" class="space-y-4" @submit.prevent="save">
        <h3 class="text-sm font-semibold">{{ getLocalizedText(product.title) }}</h3>
        <div class="grid gap-3 sm:grid-cols-2">
          <label class="space-y-1 text-sm">{{ t(key + 'scope') }}
            <select v-model.number="skuID" class="h-10 w-full rounded-md border border-border bg-background px-3" :disabled="saving || loading" @change="editScope">
              <option :value="0">{{ t(key + 'productScope') }}</option>
              <option v-for="sku in product.skus" :key="sku.id" :value="sku.id">{{ skuLabel(sku.id) }}</option>
            </select>
          </label>
          <label class="space-y-1 text-sm">{{ t(key + 'amount') }}
            <Input v-model="amount" type="number" min="0.01" step="0.01" required :disabled="saving || loading" />
          </label>
        </div>
        <div class="flex justify-end"><Button type="submit" :disabled="saving || loading">{{ t('admin.common.save') }}</Button></div>
        <div class="divide-y divide-border border-y border-border">
          <div v-for="price in ownPrices" :key="price.id" class="flex items-center justify-between gap-3 py-2 text-sm">
            <span class="min-w-0 break-words">{{ skuLabel(price.sku_id) }}</span>
            <span class="ml-auto font-mono">{{ Number(price.price_amount).toFixed(2) }}</span>
            <Button type="button" variant="ghost" size="sm" :disabled="saving || loading" :title="t('admin.common.delete')" :aria-label="t('admin.common.delete')" @click="remove(price)"><Trash2 class="h-4 w-4" /></Button>
          </div>
          <p v-if="!ownPrices.length" class="py-3 text-sm text-muted-foreground">{{ t(key + 'empty') }}</p>
        </div>
      </form>
      <p v-if="error" role="alert" class="text-sm text-destructive">{{ error }}</p>
      <div class="flex justify-end"><Button variant="outline" :disabled="saving" @click="emit('close')">{{ t('admin.common.cancel') }}</Button></div>
    </DialogScrollContent>
  </Dialog>
</template>
