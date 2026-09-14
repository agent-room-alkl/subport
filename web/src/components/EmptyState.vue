<script setup lang="ts">
defineProps<{
  title?: string
  description?: string
}>()
</script>

<template>
  <div class="card flex flex-col items-center justify-center gap-2 px-6 py-12 text-center">
    <div class="mb-1 flex h-10 w-10 items-center justify-center rounded-full border border-line bg-slate-50 text-teal shadow-glow-sm">
      <slot name="icon">
        <span class="text-lg leading-none">∅</span>
      </slot>
    </div>
    <div class="text-sm font-medium text-ink">{{ title || '暂无数据' }}</div>
    <p v-if="description" class="max-w-sm text-sm text-slatex">{{ description }}</p>
    <div v-if="$slots.actions" class="mt-3 flex gap-2">
      <slot name="actions" />
    </div>
  </div>
</template>
