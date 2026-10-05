import { useId, useState } from 'react'
import { ChevronRight } from 'lucide-react'
import type { ToolActivity } from './api'
import { toolState, type ToolMessage, type ToolState } from './chatItems'
import styles from './ToolActivity.module.css'

// Groups longer than this start collapsed to their newest call.
export const COLLAPSE_AFTER = 3

const stateLabel: Record<ToolState, string> = { running: '실행 중', completed: '완료', error: '오류', unknown: '결과 없음' }

export function ToolGroup({ tools, live }: { tools: ToolMessage[]; live: boolean }) {
  const [expanded, setExpanded] = useState(false)
  const [detail, setDetail] = useState<string | null>(null)
  const listID = useId()
  const many = tools.length > COLLAPSE_AFTER
  const shown = many && !expanded ? tools.slice(-1) : tools
  const errors = tools.filter(item => toolState(item.tool, live) === 'error').length
  return <section className={styles.group} aria-label={`도구 ${tools.length}개`}>
    {many && <button type="button" className={styles.toggle} aria-expanded={expanded} aria-controls={listID} onClick={() => setExpanded(value => !value)}>
      <ChevronRight aria-hidden size={14} className={styles.chevron} />
      <span>도구 {tools.length}개</span>
      {errors > 0 && <span className={styles.errors}>오류 {errors}</span>}
      {!expanded && <span className={styles.hint}>마지막 도구만 표시</span>}
    </button>}
    <ul id={listID} className={styles.list}>
      {shown.map(item => <ToolRow key={item.id} item={item} state={toolState(item.tool, live)} open={detail === item.id} onToggle={() => setDetail(current => current === item.id ? null : item.id)} />)}
    </ul>
  </section>
}

function ToolRow({ item, state, open, onToggle }: { item: ToolMessage; state: ToolState; open: boolean; onToggle: () => void }) {
  const detailID = useId()
  const { tool } = item
  return <li className={styles.item}>
    <button type="button" className={styles.row} aria-expanded={open} aria-controls={open ? detailID : undefined} onClick={onToggle}>
      <span className={styles.dot} data-state={state} aria-hidden />
      <span className={styles.name}>{tool.name}</span>
      {tool.summary && <span className={styles.summary}>{tool.summary}</span>}
      <span className="visually-hidden">{stateLabel[state]}</span>
    </button>
    {open && <ToolDetail id={detailID} tool={tool} state={state} />}
  </li>
}

export function ToolDetail({ id, tool, state }: { id?: string; tool: ToolActivity; state: ToolState }) {
  return <div id={id} className={styles.detail}>
    <Detail label="입력" text={tool.input} truncated={tool.input_truncated} empty="입력이 없습니다." />
    {state === 'running' ? <p className={styles.note}>실행 중입니다.</p>
      : state === 'unknown' ? <p className={styles.note}>결과가 기록되지 않았습니다.</p>
      : <Detail label={state === 'error' ? '오류' : '결과'} text={tool.result} truncated={tool.result_truncated} empty="출력이 없습니다." tone={state === 'error' ? 'error' : undefined} />}
  </div>
}

function Detail({ label, text, truncated, empty, tone }: { label: string; text: string; truncated: boolean; empty: string; tone?: 'error' }) {
  return <div className={styles.block}>
    <div className={styles.label} data-tone={tone}>{label}</div>
    {text ? <pre className={styles.pre} tabIndex={0}>{text}</pre> : <p className={styles.note}>{empty}</p>}
    {truncated && <p className={styles.note}>길어서 앞부분만 표시합니다. 전체 내용은 PC에서 확인하세요.</p>}
  </div>
}
