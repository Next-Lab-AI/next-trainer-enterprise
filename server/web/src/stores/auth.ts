import { defineStore } from "pinia"
import { ref } from "vue"

import { authApi } from "../api/auth"
import { getAuthToken, setAuthToken } from "../api/client"
import type { User } from "../api/types"

export const useAuthStore = defineStore("auth", () => {
  const user = ref<User | null>(null)
  const ready = ref(false)

  async function fetchMe() {
    if (!getAuthToken()) {
      user.value = null
      ready.value = true
      return
    }
    try {
      user.value = await authApi.me()
    } catch {
      user.value = null
    } finally {
      ready.value = true
    }
  }

  async function login(username: string, password: string) {
    user.value = await authApi.login(username, password)
  }

  async function logout() {
    await authApi.logout()
    user.value = null
  }

  function isAdmin() {
    return user.value?.role === "admin"
  }

  return { user, ready, fetchMe, login, logout, isAdmin }
})

export { setAuthToken }
