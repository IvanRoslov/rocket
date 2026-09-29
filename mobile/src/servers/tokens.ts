import * as SecureStore from 'expo-secure-store'

/** SecureStore keys may only contain [A-Za-z0-9._-]. */
export function tokenKey(baseUrl: string): string {
  return `rocket.token.${baseUrl.replace(/[^A-Za-z0-9._-]/g, '_')}`
}

export async function getToken(baseUrl: string): Promise<string | null> {
  try {
    return await SecureStore.getItemAsync(tokenKey(baseUrl))
  } catch {
    return null
  }
}

export function setToken(baseUrl: string, token: string): Promise<void> {
  return SecureStore.setItemAsync(tokenKey(baseUrl), token)
}

export async function deleteToken(baseUrl: string): Promise<void> {
  try {
    await SecureStore.deleteItemAsync(tokenKey(baseUrl))
  } catch {
    // nothing to delete
  }
}
