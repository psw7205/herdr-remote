import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'

const plugins = [remarkGfm]
const components: Components = {
  img({ alt }) { return <span className="image-placeholder">[이미지: {alt || '미리보기 없음'}]</span> },
  a({ node: _node, children, ...props }) { return <a {...props} target="_blank" rel="noopener noreferrer">{children}</a> },
}

export function MessageMarkdown({ text }: { text: string }) {
  return <ReactMarkdown remarkPlugins={plugins} components={components} skipHtml>{text}</ReactMarkdown>
}
