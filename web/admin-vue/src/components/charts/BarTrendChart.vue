<script setup lang="ts">
import { computed } from 'vue'

export type BarPoint = { label: string; value: number }

const props = withDefaults(
  defineProps<{
    points: BarPoint[]
    color?: string
    height?: number
  }>(),
  { color: '#0F9F73', height: 160 },
)

const pad = { top: 16, right: 8, bottom: 28, left: 8 }
const width = 360

const maxVal = computed(() => {
  const m = Math.max(0, ...props.points.map((p) => Math.max(0, Number(p.value) || 0)))
  return m > 0 ? m : 1
})

const chartH = computed(() => props.height - pad.top - pad.bottom)
const chartW = computed(() => width - pad.left - pad.right)

const bars = computed(() => {
  const n = Math.max(props.points.length, 1)
  const gap = 6
  const bw = Math.max(8, (chartW.value - gap * (n - 1)) / n)
  return props.points.map((p, i) => {
    const v = Math.max(0, Number(p.value) || 0)
    const h = (v / maxVal.value) * chartH.value
    const x = pad.left + i * (bw + gap)
    const y = pad.top + chartH.value - h
    return { ...p, value: v, x, y, w: bw, h, labelX: x + bw / 2 }
  })
})

const empty = computed(() => props.points.every((p) => (Number(p.value) || 0) <= 0))
</script>

<template>
  <div class="w-full overflow-x-auto">
    <svg
      :viewBox="`0 0 ${width} ${height}`"
      class="mx-auto block w-full max-w-md"
      role="img"
      aria-label="近7日用量"
    >
      <line
        :x1="pad.left"
        :x2="width - pad.right"
        :y1="pad.top + chartH"
        :y2="pad.top + chartH"
        stroke="#DCE4EA"
        stroke-width="1"
      />
      <template v-if="!empty">
        <g v-for="(b, i) in bars" :key="i">
          <rect
            :x="b.x"
            :y="b.y"
            :width="b.w"
            :height="Math.max(b.h, b.value > 0 ? 2 : 0)"
            :fill="color"
            rx="3"
            ry="3"
            opacity="0.92"
          >
            <title>{{ b.label }}: {{ b.value }}</title>
          </rect>
          <text
            :x="b.labelX"
            :y="height - 8"
            text-anchor="middle"
            fill="#64748B"
            font-size="10"
          >
            {{ b.label }}
          </text>
          <text
            v-if="b.value > 0"
            :x="b.labelX"
            :y="b.y - 4"
            text-anchor="middle"
            fill="#0B1220"
            font-size="9"
            font-weight="600"
          >
            {{ b.value }}
          </text>
        </g>
      </template>
      <text
        v-else
        :x="width / 2"
        :y="pad.top + chartH / 2"
        text-anchor="middle"
        fill="#64748B"
        font-size="12"
      >
        暂无用量数据
      </text>
    </svg>
  </div>
</template>
