import { describe, expect, it } from 'vitest'
import { singleFlight } from './singleFlight'

describe('singleFlight', () => {
  it('drops calls while one is pending and runs again after it settles', async () => {
    const pending: Array<() => void> = []
    let calls = 0
    const run = singleFlight(() => { calls++; return new Promise<void>(resolve => pending.push(resolve)) })
    const first = run()
    await run()
    expect(calls).toBe(1)
    pending[0]()
    await first
    const next = run()
    expect(calls).toBe(2)
    pending[1]()
    await next
  })

  it('releases the guard when the task fails', async () => {
    let calls = 0
    const run = singleFlight(async () => { calls++; throw new Error('read failed') })
    await expect(run()).rejects.toThrow('read failed')
    await expect(run()).rejects.toThrow('read failed')
    expect(calls).toBe(2)
  })
})
