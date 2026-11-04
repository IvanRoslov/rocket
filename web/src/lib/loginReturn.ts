/** Separate applications must load their own document after shared pairing. */
export function returnAfterLogin(
  next: string,
  navigate: (path: string) => void,
  load: (path: string) => void = (path) => window.location.assign(path),
): void {
  if (next === "/creative" || next.startsWith("/creative/")) load(next);
  else navigate(next);
}
