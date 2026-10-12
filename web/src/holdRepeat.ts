export type Hold = { release(): void; cancel(): void }

export function startHold({ send, delay, interval }: { send: () => Promise<boolean>; delay: number; interval: number }): Hold {
  let held = true
  let cancelled = false
  let cancelWait = () => {}
  const wait = (ms: number) => new Promise<void>(resolve => {
    const timer = setTimeout(resolve, ms)
    cancelWait = () => { clearTimeout(timer); resolve() }
  })
  void (async () => {
    await wait(delay)
    while (!cancelled) {
      const [accepted] = await Promise.all([send().catch(() => false), wait(interval)])
      if (!accepted || !held) return
    }
  })()
  const release = () => { held = false; cancelWait() }
  return { release, cancel() { cancelled = true; release() } }
}
