const TOKEN_KEY = 'pennant_access_token'
const REFRESH_KEY = 'pennant_refresh_token'

export const authStorage = {
  getToken: () => localStorage.getItem(TOKEN_KEY),
  setTokens: (access: string, refresh: string) => {
    localStorage.setItem(TOKEN_KEY, access)
    localStorage.setItem(REFRESH_KEY, refresh)
  },
  clearTokens: () => {
    localStorage.removeItem(TOKEN_KEY)
    localStorage.removeItem(REFRESH_KEY)
  },
  getRefreshToken: () => localStorage.getItem(REFRESH_KEY),
}

export async function login(email: string, password: string): Promise<void> {
  const res = await fetch('/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password }),
  })
  if (!res.ok) throw new Error('Invalid credentials')
  const data = await res.json()
  authStorage.setTokens(data.access_token, data.refresh_token ?? '')
}

export async function refreshToken(): Promise<boolean> {
  const rt = authStorage.getRefreshToken()
  if (!rt) return false
  const res = await fetch('/auth/refresh', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ refresh_token: rt }),
  })
  if (!res.ok) {
    authStorage.clearTokens()
    return false
  }
  const data = await res.json()
  authStorage.setTokens(data.access_token, data.refresh_token ?? '')
  return true
}

export function logout() {
  authStorage.clearTokens()
  window.location.reload()
}
