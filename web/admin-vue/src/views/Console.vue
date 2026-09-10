<script setup lang="ts">
import { onMounted } from 'vue'
import { useRouter } from 'vue-router'
const router = useRouter()
onMounted(() => {
  router.replace({ name: 'console-home' })
})
</script>
<template>
  <div class="p-6 text-slatex">正在跳转到用户控制台…</div>
</template>
