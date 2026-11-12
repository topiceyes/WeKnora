/** A reader-local cache: no persistence or sharing between readers/accounts. */
export function createDocumentReadCache<T>(fetchValue: (key: string) => Promise<T>, options: {
  now?: () => number; ttl?: number; maxEntries?: number; maxChars?: number
} = {}) {
  const now = options.now ?? Date.now
  const entries = new Map<string, { value: T; expires: number; size: number }>()
  const pending = new Map<string, Promise<T>>()
  let epoch = 0, chars = 0
  const remove = (key: string) => {
    chars -= entries.get(key)?.size ?? 0
    entries.delete(key)
  }
  function peek(key: string): T | undefined {
    const entry = entries.get(key)
    if (!entry) return
    if (entry.expires <= now()) { remove(key); return }
    entries.delete(key); entries.set(key, entry)
    return entry.value
  }
  function load(key: string, speculative = false): Promise<T> | undefined {
    const cached = peek(key)
    if (cached !== undefined) return Promise.resolve(cached)
    if (pending.has(key)) return pending.get(key)!
    // A directory sweep must not flood the network. Selected documents bypass this limit.
    if (speculative && pending.size >= 2) return
    const ticket = epoch
    const request = Promise.resolve().then(() => fetchValue(key)).then(value => {
      if (ticket !== epoch) return value
      const size = JSON.stringify(value).length
      const maxChars = options.maxChars ?? 2_000_000
      if (size <= maxChars) {
        remove(key)
        entries.set(key, { value, size, expires: now() + (options.ttl ?? 15_000) })
        chars += size
        while (entries.size > (options.maxEntries ?? 8) || chars > maxChars) remove(entries.keys().next().value!)
      }
      return value
    }).finally(() => { if (pending.get(key) === request) pending.delete(key) })
    pending.set(key, request)
    return request
  }
  function clear() { epoch++; entries.clear(); pending.clear(); chars = 0 }
  return { peek, load, clear }
}
