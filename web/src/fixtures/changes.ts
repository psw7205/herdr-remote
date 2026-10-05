// Anonymous Changed Files samples for the fixture page. Every path, branch
// and line is invented; the shapes follow internal/gitstate.
import type { ChangedFile, Changes, FileDiff } from '../api'

export type FixtureChanges = { changes: Changes; diffs: Record<string, FileDiff> } | { error: string; status: number }

const file = (path: string, status: ChangedFile['status'], additions: number | null, deletions: number | null, extra: Partial<ChangedFile> = {}): ChangedFile => ({ path, status, additions, deletions, ...extra })
const text = (f: ChangedFile, diff: string, truncated = false): FileDiff => ({ ...f, content: 'text', diff, truncated })

function repository(branch: string, files: ChangedFile[], diffs: FileDiff[], extra: Partial<Changes> = {}): FixtureChanges {
  return { changes: { repository: true, branch, files, total: files.length, truncated: false, ...extra }, diffs: Object.fromEntries(diffs.map(d => [d.path, d])) }
}

const lineChart = file('src/charts/LineChart.tsx', 'modified', 6, 4)
const downsample = file('src/charts/downsample.ts', 'added', 9, 0, { untracked: true })
const legacy = file('src/charts/legacyRenderer.ts', 'deleted', 0, 4)
const cache = file('src/charts/ChartCache.ts', 'renamed', 1, 1, { old_path: 'src/charts/cache.ts' })
const preview = file('public/preview.png', 'modified', null, null, { binary: true })
const link = file('config/local.json', 'added', null, null, { untracked: true })
const notes = file('docs/rendering-notes.md', 'modified', 1, 1)

const dashboard = repository('perf/chart-render', [preview, cache, lineChart, downsample, legacy, notes, link], [
  text(lineChart, `diff --git a/src/charts/LineChart.tsx b/src/charts/LineChart.tsx
index 3f1a2b0..9c4d7e1 100644
--- a/src/charts/LineChart.tsx
+++ b/src/charts/LineChart.tsx
@@ -1,5 +1,6 @@
 import { useMemo, useRef } from 'react'
-import { drawPath } from './legacyRenderer'
+import { downsample } from './downsample'
+import { ChartCache } from './ChartCache'
 import type { Point } from './types'

 export function LineChart({ points, width }: { points: Point[]; width: number }) {
@@ -18,9 +19,11 @@ export function LineChart({ points, width }: { points: Point[]; width: number })
   const canvas = useRef<HTMLCanvasElement>(null)
-  const path = useMemo(() => drawPath(points), [points])
+  const visible = useMemo(() => downsample(points, width), [points, width])
+  const cache = useMemo(() => new ChartCache(), [])

   useEffect(() => {
-    render(canvas.current, path)
-  }, [path])
+    const frame = cache.get(width) ?? cache.put(width, visible)
+    render(canvas.current, frame)
+  }, [cache, visible, width])

   return <canvas ref={canvas} width={width} height={240} />
 }
`),
  text(downsample, `--- /dev/null
+++ b/src/charts/downsample.ts
@@ -0,0 +1,9 @@
+import type { Point } from './types'
+
+export function downsample(points: Point[], width: number): Point[] {
+  const bucket = Math.ceil(points.length / width)
+  if (bucket <= 1) return points
+  const out: Point[] = []
+  for (let start = 0; start < points.length; start += bucket) out.push(points[start])
+  return out
+}
`),
  text(legacy, `diff --git a/src/charts/legacyRenderer.ts b/src/charts/legacyRenderer.ts
deleted file mode 100644
index 7a0c3d1..0000000
--- a/src/charts/legacyRenderer.ts
+++ /dev/null
@@ -1,4 +0,0 @@
-import type { Point } from './types'
-export function drawPath(points: Point[]): string {
-  return points.map(({ x, y }, i) => \`\${i ? 'L' : 'M'}\${x},\${y}\`).join(' ')
-}
`),
  text(cache, `diff --git a/src/charts/cache.ts b/src/charts/ChartCache.ts
similarity index 86%
rename from src/charts/cache.ts
rename to src/charts/ChartCache.ts
index 1b2c3d4..5e6f7a8 100644
--- a/src/charts/cache.ts
+++ b/src/charts/ChartCache.ts
@@ -1,4 +1,4 @@
-export class Cache {
+export class ChartCache {
   private frames = new Map<number, ImageData>()
   get(width: number) { return this.frames.get(width) }
   put(width: number, frame: ImageData) { this.frames.set(width, frame); return frame }
`),
  { ...preview, content: 'binary', diff: '', truncated: false },
  { ...link, content: 'none', diff: '', truncated: false },
  text(notes, `diff --git a/docs/rendering-notes.md b/docs/rendering-notes.md
index 0a1b2c3..4d5e6f7 100644
--- a/docs/rendering-notes.md
+++ b/docs/rendering-notes.md
@@ -3 +3 @@
-| 첫 render | 1,840ms | canvas 전환 전 측정값이며 확대/축소를 반복할 때마다 전체 path를 다시 계산하므로 main thread가 오래 막힌다 |
+| 첫 render | 210ms | canvas로 전환하고 1만 건 이상은 LTTB로 줄인 뒤 그린 측정값이며 폭이 바뀔 때만 ResizeObserver에서 다시 계산한다 |
\\ No newline at end of file
`),
])

const apiFile = file('tests/payments.failure.test.ts', 'modified', 3, 0)
const api = repository('main', [apiFile], [
  text(apiFile, `diff --git a/tests/payments.failure.test.ts b/tests/payments.failure.test.ts
index 2c3d4e5..6f7a8b9 100644
--- a/tests/payments.failure.test.ts
+++ b/tests/payments.failure.test.ts
@@ -40,2 +40,5 @@ describe('payment failures', () => {
   it('rejects an expired card', () => expectDecline('expired'))
+  it('rejects an insufficient balance', () => expectDecline('insufficient'))
+  it('retries a gateway timeout once', () => expectRetry('timeout', 1))
+  it('reports a duplicate charge', () => expectDecline('duplicate'))
 })
`),
])

// changes-large: a listing past the Bridge's file cap and a diff past its byte cap.
function large(): FixtureChanges {
  const files = Array.from({ length: 500 }, (_, i) => file(`generated/locale/messages-${String(i).padStart(3, '0')}.json`, 'modified', 2, 2))
  const body = Array.from({ length: 1200 }, (_, i) => `+  "key.${i}": "번역 문자열 ${i}",`).join('\n')
  const first = files[0]
  return repository('chore/locale-sync', files, [text(first, `diff --git a/${first.path} b/${first.path}\n--- a/${first.path}\n+++ b/${first.path}\n@@ -1,2 +1,1202 @@\n {\n${body}\n`, true)], { total: 1840, truncated: true })
}

export function fixtureChanges(scenario: string): Record<string, FixtureChanges> {
  const out: Record<string, FixtureChanges> = {
    'claude:fixture-working': dashboard,
    'claude:fixture-attention': api,
    'pane:w3:p2': { changes: { repository: false, files: [], total: 0, truncated: false }, diffs: {} },
    'claude:fixture-unknown': { error: 'GIT_TIMEOUT', status: 504 },
    'claude:fixture-unverified': { error: 'PROJECT_NOT_FOUND', status: 404 },
  }
  if (scenario === 'changes-large') out['claude:fixture-working'] = large()
  return out
}
