<script setup lang="ts">
import { nextTick, onMounted, ref } from 'vue'
import PageHeader from '../components/PageHeader.vue'

type Msg = { role: 'user' | 'assistant' | 'system'; content: string }

const KEY_LS = 'subport_playground_key'
const MODEL_LS = 'subport_playground_model'

const apiKey = ref('')
const model = ref('gpt-4o-mini')
const input = ref('')
const messages = ref<Msg[]>([])
const sending = ref(false)
const err = ref('')
const listEl = ref<HTMLElement | null>(null)

onMounted(() => {
  try {
    apiKey.value = localStorage.getItem(KEY_LS) || ''
    model.value = localStorage.getItem(MODEL_LS) || 'gpt-4o-mini'
  } catch { /* ignore */ }
})

function persist() {
  try {
    localStorage.setItem(KEY_LS, apiKey.value.trim())
    localStorage.setItem(MODEL_LS, model.value.trim() || 'gpt-4o-mini')
  } catch { /* ignore */ }
}

async function scrollBottom() {
  await nextTick()
  if (listEl.value) listEl.value.scrollTop = listEl.value.scrollHeight
}

async function send() {
  err.value = ''
  const text = input.value.trim()
  if (!text) return
  if (!apiKey.value.trim()) {
    err.value = '请填写 sk-sp- API 密钥（在控制台「密钥」页创建）'
    return
  }
  persist()
  messages.value.push({ role: 'user', content: text })
  input.value = ''
  sending.value = true
  await scrollBottom()
  try {
    const body = {
      model: model.value.trim() || 'gpt-4o-mini',
      messages: messages.value.map((m) => ({ role: m.role, content: m.content })),
      stream: false,
    }
    const res = await fetch('/v1/chat/completions', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${apiKey.value.trim()}`,
      },
      body: JSON.stringify(body),
    })
    const raw = await res.text()
    let data: any = null
    try { data = raw ? JSON.parse(raw) : null } catch { /* ignore */ }
    if (!res.ok) {
      const msg = data?.error || data?.message || raw || `${res.status} ${res.statusText}`
      throw new Error(typeof msg === 'string' ? msg : JSON.stringify(msg))
    }
    const content =
      data?.choices?.[0]?.message?.content ??
      data?.choices?.[0]?.text ??
      data?.content ??
      '(空响应)'
    messages.value.push({ role: 'assistant', content: String(content) })
  } catch (e: any) {
    err.value = e?.message || '请求失败'
    messages.value.push({ role: 'assistant', content: `错误：${err.value}` })
  } finally {
    sending.value = false
    await scrollBottom()
  }
}

function clearChat() {
  messages.value = []
  err.value = ''
}
</script>

<template>
  <div class="flex h-[calc(100vh-8rem)] min-h-[28rem] flex-col">
    <PageHeader title="对话" subtitle="网关 Playground · POST /v1/chat/completions">
      <template #actions>
        <button class="btn-ghost" type="button" @click="clearChat">清空</button>
      </template>
    </PageHeader>

    <div class="card mb-3 grid gap-3 sm:grid-cols-2">
      <div>
        <label class="label">API 密钥</label>
        <input
          v-model="apiKey"
          class="input font-mono text-sm"
          type="password"
          placeholder="sk-sp-…"
          autocomplete="off"
          @change="persist"
        />
      </div>
      <div>
        <label class="label">模型</label>
        <input
          v-model="model"
          class="input font-mono text-sm"
          placeholder="gpt-4o-mini"
          @change="persist"
        />
      </div>
    </div>

    <p v-if="err" class="mb-2 text-sm text-red-600">{{ err }}</p>

    <div ref="listEl" class="card mb-3 flex-1 space-y-3 overflow-y-auto">
      <p v-if="messages.length === 0" class="text-sm text-slatex">
        输入消息开始对话。密钥仅保存在本机浏览器，不会上传到管理会话。
      </p>
      <div
        v-for="(m, i) in messages"
        :key="i"
        class="rounded-lg px-3 py-2 text-sm"
        :class="m.role === 'user' ? 'bg-teal/10 ml-8' : 'bg-canvas mr-8'"
      >
        <div class="mb-1 text-[11px] font-semibold uppercase tracking-wide text-slatex">
          {{ m.role === 'user' ? '你' : '助手' }}
        </div>
        <div class="whitespace-pre-wrap break-words">{{ m.content }}</div>
      </div>
    </div>

    <div class="flex gap-2">
      <textarea
        v-model="input"
        class="input min-h-[3.5rem] flex-1 resize-y"
        rows="2"
        placeholder="输入消息，Enter 发送（Shift+Enter 换行）"
        :disabled="sending"
        @keydown.enter.exact.prevent="send"
      />
      <button class="btn-primary self-end" type="button" :disabled="sending" @click="send">
        {{ sending ? '发送中…' : '发送' }}
      </button>
    </div>
  </div>
</template>
