import { SquareTerminal } from 'lucide-react'
import type { ConditionalInput, Session } from './api'
import { Notice } from './Notice'
import { formatRoute } from './route'
import { agentName, groupSessions, plainPreview, projectName, rowNote, statusPresentation } from './presentation'
import { StatusMark } from './StatusBadge'
import { relativeTime } from './time'
import { TopBar } from './TopBar'
import styles from './SessionList.module.css'

export function SessionList({ sessions, loaded, error, conditionalInput, onOpen }: { sessions: Session[]; loaded: boolean; error: string; conditionalInput: ConditionalInput; onOpen: (session: Session) => void }) {
  const groups = groupSessions(sessions)
  const now = Date.now()
  return <div className={styles.screen}>
    <TopBar title="세션" subtitle={loaded && !error ? <span>{sessions.length > 0 ? `${sessions.length}개 실행 중` : 'Herdr 연결됨'}</span> : undefined} />
    {(error || conditionalInput === 'unsupported') && <div className={styles.notices}>
      {error && <Notice tone="danger">{error}</Notice>}
      {conditionalInput === 'unsupported' && <Notice tone="warning">이 Herdr는 원격 입력을 지원하지 않아 Chat 입력과 Terminal을 쓸 수 없습니다. agent는 계속 실행 중입니다. PC에서 Herdr patch를 확인하세요.</Notice>}
    </div>}
    <main className={styles.list} aria-busy={!loaded} aria-label="세션 목록">
      {!loaded && <div className={styles.skeleton} aria-hidden="true">{[0, 1, 2].map(index => <span key={index}><i /><b /><em /></span>)}</div>}
      {loaded && groups.length === 0 && !error && <div className={styles.empty}>
        <SquareTerminal aria-hidden size={28} />
        <strong>실행 중인 agent가 없습니다.</strong>
        <p>PC의 Herdr에서 agent를 실행하면 여기에 나타납니다.</p>
      </div>}
      {groups.map(group => <section key={group.key} className={styles.group} aria-labelledby={`group-${group.key}`}>
        <h2 id={`group-${group.key}`} className={styles.groupTitle}>{group.title}<span className={styles.count}>{group.sessions.length}</span></h2>
        <ul className={styles.rows}>
          {group.sessions.map(item => {
            const status = statusPresentation(item)
            const when = item.last_activity ? relativeTime(item.last_activity, now) : ''
            const note = rowNote(item)
            const preview = item.last_message ? plainPreview(item.last_message.text) : ''
            return <li key={item.id}>
              {/* A real link keeps long-press and modifier-click behavior; a
                  plain tap goes through the app's history instead. */}
              <a className={styles.row} href={formatRoute({ session: item.id, view: 'chat' })} onClick={event => {
                if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
                event.preventDefault()
                onOpen(item)
              }}>
                <StatusMark tone={status.tone} size="large" />
                <span className={styles.body}>
                  <span className={styles.head}>
                    <strong className={styles.title}>{item.title || agentName(item.agent)}</strong>
                    {when && <time className={styles.time} dateTime={item.last_activity}>{when}</time>}
                  </span>
                  <span className={styles.meta} data-tone={status.tone}>
                    {status.label === group.title ? <span className="visually-hidden">{status.label}</span> : <span className={styles.state}>{status.label}</span>}
                    <span className={styles.project}>{projectName(item.project)}</span>
                    {note && <span>{note}</span>}
                  </span>
                  {preview && <span className={styles.preview}>{item.last_message?.role === 'user' ? `나: ${preview}` : preview}</span>}
                </span>
              </a>
            </li>
          })}
        </ul>
      </section>)}
    </main>
  </div>
}
