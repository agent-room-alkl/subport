import { defineStore } from 'pinia'
import { ref } from 'vue'
import { authApi, getToken, setToken } from '../api/client'

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string | null>(getToken())
  const user = ref<any>(null)

  async function login(username: string, password: string) {
    const out = await authApi.login(username, password)
    token.value = out.token
    user.value = out.user
    setToken(out.token)
    try { localStorage.setItem('subport_user', JSON.stringify(out.user)) } catch { /* ignore */ }
  }

  function logout() {
    token.value = null
    user.value = null
    setToken(null)
    try { localStorage.removeItem('subport_user') } catch { /* ignore */ }
  }

  try {
    const raw = localStorage.getItem('subport_user')
    if (raw) user.value = JSON.parse(raw)
  } catch { /* ignore */ }

  return { token, user, login, logout }
})
