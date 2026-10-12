import { apiRequest, setAuthToken } from "./client"
import type { User } from "./types"

export const authApi = {
  async login(username: string, password: string): Promise<User> {
    const resp = await apiRequest<{ token: string; user: User }>("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    })
    setAuthToken(resp.token)
    return resp.user
  },
  async logout(): Promise<void> {
    try {
      await apiRequest("/api/auth/logout", { method: "POST", body: "{}" })
    } catch {
    } finally {
      setAuthToken(null)
    }
  },
  me: () => apiRequest<User>("/api/auth/me"),
  changePassword: (oldPassword: string, newPassword: string) =>
    apiRequest("/api/auth/password", {
      method: "POST",
      body: JSON.stringify({ old_password: oldPassword, new_password: newPassword }),
    }),
  bootstrap: (username: string, password: string) =>
    apiRequest<User>("/api/bootstrap", { method: "POST", body: JSON.stringify({ username, password }) }),
}
