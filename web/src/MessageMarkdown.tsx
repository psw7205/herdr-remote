import { Children, isValidElement, memo, useEffect, useRef, useState, type ReactElement, type ReactNode } from 'react'
import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Check, Copy, Image } from 'lucide-react'
import styles from './MessageMarkdown.module.css'

function textOf(node: ReactNode): string {
  if (typeof node === 'string' || typeof node === 'number') return String(node)
  if (Array.isArray(node)) return node.map(textOf).join('')
  if (isValidElement<{ children?: ReactNode }>(node)) return textOf(node.props.children)
  return ''
}

function CodeBlock({ children }: { children?: ReactNode }) {
  const code = Children.toArray(children).find(isValidElement) as ReactElement<{ className?: string }> | undefined
  const language = /language-([\w#+.-]+)/.exec(code?.props.className ?? '')?.[1]
  const [copied, setCopied] = useState<'idle' | 'done' | 'failed'>('idle')
  const reset = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  useEffect(() => () => clearTimeout(reset.current), [])
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(textOf(children).replace(/\n$/, ''))
      setCopied('done')
    } catch { setCopied('failed') }
    clearTimeout(reset.current)
    reset.current = setTimeout(() => setCopied('idle'), 1600)
  }
  return <figure className={styles.codeBlock}>
    <figcaption className={styles.codeHeader}>
      <span translate="no">{language ?? 'code'}</span>
      <button type="button" className={styles.copy} onClick={() => void copy()} aria-label="코드 복사">
        {copied === 'done' ? <Check aria-hidden size={15} /> : <Copy aria-hidden size={15} />}
        <span aria-live="polite">{copied === 'done' ? '복사됨' : copied === 'failed' ? '복사 실패' : '복사'}</span>
      </button>
    </figcaption>
    <pre translate="no">{children}</pre>
  </figure>
}

const plugins = [remarkGfm]
const components: Components = {
  img({ alt }) { return <span className={styles.image}><Image aria-hidden size={14} />이미지: {alt || '미리보기 없음'}</span> },
  a({ node: _node, children, ...props }) { return <a {...props} target="_blank" rel="noopener noreferrer">{children}</a> },
  pre({ children }) { return <CodeBlock>{children}</CodeBlock> },
  code({ node: _node, ...props }) { return <code {...props} translate="no" /> },
  table({ node: _node, ...props }) { return <div className={styles.tableScroll} data-scroll="x"><table {...props} /></div> },
}

// A new message re-renders the list; memo keeps earlier messages from being re-parsed.
export const MessageMarkdown = memo(function MessageMarkdown({ text }: { text: string }) {
  return <div className={styles.prose}><ReactMarkdown remarkPlugins={plugins} components={components} skipHtml>{text}</ReactMarkdown></div>
})
