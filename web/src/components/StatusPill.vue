<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    status?: string
    label?: string
  }>(),
  { status: 'muted', label: '' },
)

const cls = computed(() => {
  const s = (props.status || '').toLowerCase()
  if (['ok', 'healthy', 'active', 'enabled', 'success'].includes(s)) return 'pill-ok'
  if (['warn', 'warning', 'degraded', 'pending'].includes(s)) return 'pill-warn'
  if (['err', 'error', 'failed', 'disabled', 'unhealthy'].includes(s)) return 'pill-err'
  if (['info', 'running'].includes(s)) return 'pill-info'
  return 'pill-muted'
})

const text = computed(() => props.label || props.status || '—')
</script>

<template>
  <span :class="cls">{{ text }}</span>
</template>
