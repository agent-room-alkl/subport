<script setup lang="ts">
import { computed } from 'vue'

export type DonutSegment = { label: string; value: number; color: string }

const props = withDefaults(
  defineProps<{
    segments: DonutSegment[]
    size?: number
    thickness?: number
    centerText?: string
    centerSub?: string
  }>(),
  { size: 168, thickness: 22 },
)

const total = computed(() =>
  props.segments.reduce((sum, s) => sum + Math.max(0, Number(s.value) || 0), 0),
)

const radius = computed(() => props.size / 2 - props.thickness / 2 - 2)
const circumference = computed(() => 2 * Math.PI * radius.value)
const cx = computed(() => props.size / 2)
const cy = computed(() => props.size / 2)

const arcs = computed(() => {
  const t = total.value
  const circ = circumference.value
  let offset = 0
  const out: { label: string; value: number; color: string; dash: string; offset: number }[] = []
  for (const s of props.segments) {
    const v = Math.max(0, Number(s.value) || 0)
    if (v <= 0) continue
    const len = t > 0 ? (v / t) * circ : 0
    out.push({
      label: s.label,
      value: v,
      color: s.color,
      dash: `${len} ${Math.max(0, circ - len)}`,
      offset: -offset,
    })
    offset += len
  }
  return out
})

const legend = computed(() =>
  props.segments.map((s) => ({
    ...s,
    value: Math.max(0, Number(s.value) || 0),
  })),
)
</script>

<template>
  <div class="flex flex-wrap items-center gap-4">
    <div class="relative shrink-0" :style="{ width: size + 'px', height: size + 'px' }">
      <svg :width="size" :height="size" :viewBox="`0 0 ${size} ${size}`" class="block">
        <circle
          :cx="cx"
          :cy="cy"
          :r="radius"
          fill="none"
          stroke="#E2E8F0"
          :stroke-width="thickness"
        />
        <circle
          v-for="(a, i) in arcs"
          :key="i"
          :cx="cx"
          :cy="cy"
          :r="radius"
          fill="none"
          :stroke="a.color"
          :stroke-width="thickness"
          stroke-linecap="butt"
          :stroke-dasharray="a.dash"
          :stroke-dashoffset="a.offset"
          :style="{ transform: `rotate(-90deg)`, transformOrigin: `${cx}px ${cy}px` }"
        />
      </svg>
      <div
        class="pointer-events-none absolute inset-0 flex flex-col items-center justify-center text-center"
      >
        <div v-if="centerText" class="text-lg font-semibold tabular-nums leading-tight text-ink">
          {{ centerText }}
        </div>
        <div v-if="centerSub" class="mt-0.5 text-[11px] text-slatex">{{ centerSub }}</div>
      </div>
    </div>
    <ul class="min-w-[8rem] space-y-1.5 text-sm">
      <li v-for="(s, i) in legend" :key="i" class="flex items-center justify-between gap-3">
        <span class="inline-flex items-center gap-2 text-slatex">
          <span
            class="inline-block h-2.5 w-2.5 rounded-full"
            :style="{ background: s.color }"
          />
          {{ s.label }}
        </span>
        <span class="font-medium tabular-nums text-ink">{{ s.value }}</span>
      </li>
    </ul>
  </div>
</template>
