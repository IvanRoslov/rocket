/** Existing root pages, shared by the development and Creative apps. */
export function termPagePath(sessionId: string): string {
  return `/term/${encodeURIComponent(sessionId)}`
}

export function chatPagePath(sessionId: string): string {
  return `/chat/${encodeURIComponent(sessionId)}`
}
