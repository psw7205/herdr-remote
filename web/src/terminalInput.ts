const DEL = '\x7f'
const IME_KEY_CODE = 229
const MODIFIER_KEYS = new Set(['Shift', 'Control', 'Alt', 'Meta', 'CapsLock'])
// Keys xterm turns into terminal input itself (and cancels in the browser).
const XTERM_KEYS = new Set(['Backspace', 'Enter', 'Tab', 'Escape', 'ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight', 'Delete', 'Home', 'End', 'PageUp', 'PageDown'])

// Keystrokes that turn the field's last sent value into `next`: delete back to
// the first differing code point, then retype the rest. A terminal line has no
// cursor to move, so an edit in the middle retypes everything after it.
export function editFor(known: string, next: string): string {
  const before = Array.from(known)
  const after = Array.from(next)
  let prefix = 0
  while (prefix < before.length && prefix < after.length && before[prefix] === after[prefix]) prefix++
  return DEL.repeat(before.length - prefix) + after.slice(prefix).join('')
}

export type ImeField = { value: string }
export type ImeKey = { keyCode: number; key: string; ctrlKey: boolean; metaKey?: boolean }

export type ImeController = {
  keydown(key: ImeKey): boolean
  keyup(): void
  compositionstart(): void
  compositionend(): void
  beforeinput(inputType: string): boolean
  input(): void
  blur(): void
  paste(): void
  reset(): void
}

// Keyboards report IME-driven keys as 229, some as 0 or an unidentified key.
function isImeKey({ keyCode, key }: ImeKey): boolean {
  return keyCode === IME_KEY_CODE || keyCode === 0 || key === 'Unidentified' || key === 'Process'
}
function xtermSends(key: ImeKey): boolean {
  if (isImeKey(key)) return false
  return XTERM_KEYS.has(key.key) || (key.key.length === 1 && !key.ctrlKey && !key.metaKey)
}

// Turns what an IME does to xterm's hidden textarea into terminal input.
// xterm's own helper diffs the textarea around every keyCode 229 keydown and
// resends its whole value when the IME edits earlier text in place (xterm.js
// #3600, #6078), so the field is diffed here against the last value sent.
export function createImeController({ field, emit }: { field: ImeField; emit: (text: string) => void }): ImeController {
  let known = ''
  let composing = false
  let sentByXterm = false

  const flush = () => {
    const next = field.value
    const data = editFor(known, next)
    if (!data) { known = next; return }
    if (next.includes('\n')) {
      emit(data.replace(/\n/g, '\r'))
      field.value = ''
      known = ''
      return
    }
    emit(data)
    known = next
  }
  const clear = () => {
    flush()
    known = ''
  }

  return {
    keydown(key) {
      sentByXterm = xtermSends(key)
      if (isImeKey(key)) return false
      // Backspace inside a composition edits the composing text in the browser;
      // the net edit goes out when the composition ends.
      if (composing && key.key === 'Backspace') { sentByXterm = false; return false }
      // xterm sends CR or ETX and empties the textarea right after this returns.
      if (key.key === 'Enter' || key.keyCode === 13 || (key.ctrlKey && key.key.toLowerCase() === 'c')) clear()
      // Any other real key goes to xterm next, so a composition is sent before it.
      else if (composing && !MODIFIER_KEYS.has(key.key)) flush()
      return true
    },
    keyup() { sentByXterm = false },
    compositionstart() { composing = true },
    compositionend() { composing = false; flush() },
    beforeinput(inputType) {
      if (inputType !== 'deleteContentBackward' || composing || field.value !== '') return false
      emit(DEL)
      return true
    },
    input() {
      if (composing) return
      // A character xterm already sent from a real keydown lands in the field too.
      if (sentByXterm) { known = field.value; return }
      flush()
    },
    blur() { known = '' },
    paste() { known = '' },
    reset() {
      flush()
      field.value = ''
      known = ''
      composing = false
    },
  }
}

type ImeTerminal = {
  textarea: HTMLTextAreaElement | undefined
  element: HTMLElement | undefined
  options: { fontFamily?: string; fontSize?: number }
  attachCustomKeyEventHandler(handler: (event: KeyboardEvent) => boolean): void
}

const OWNED_EVENTS = ['compositionstart', 'compositionupdate', 'compositionend', 'beforeinput', 'input'] as const

// Owns the textarea's composition and input events. They are caught on the
// terminal element in the capture phase and stopped there, before xterm's own
// listeners on the textarea run; keydown still reaches xterm for real keys.
// The composing text is previewed in xterm's composition view, which xterm no
// longer fills, at the textarea (xterm keeps that on the cursor cell).
export function attachImeInput(term: ImeTerminal, emit: (text: string) => void): { reset(): void; dispose(): void } {
  const textarea = term.textarea
  const host = term.element
  if (!textarea || !host) return { reset() {}, dispose() {} }
  const ime = createImeController({ field: textarea, emit })
  const view = host.querySelector<HTMLElement>('.composition-view')
  const preview = (text: string) => {
    if (!view) return
    view.textContent = text
    view.classList.toggle('active', text.length > 0)
    if (!text) return
    const { left, top, height, lineHeight } = textarea.style
    Object.assign(view.style, { left, top, height, lineHeight, fontFamily: term.options.fontFamily ?? '', fontSize: term.options.fontSize ? `${term.options.fontSize}px` : '' })
  }
  term.attachCustomKeyEventHandler(event => {
    if (event.type === 'keydown') return ime.keydown(event)
    if (event.type === 'keyup') ime.keyup()
    return true
  })
  const owned = (event: Event) => {
    if (event.target !== textarea) return
    event.stopPropagation()
    switch (event.type) {
      case 'compositionstart': ime.compositionstart(); break
      case 'compositionupdate': preview((event as CompositionEvent).data ?? ''); break
      case 'compositionend': ime.compositionend(); preview(''); break
      case 'beforeinput': if (ime.beforeinput((event as InputEvent).inputType)) event.preventDefault(); break
      case 'input': ime.input(); break
    }
  }
  const onBlur = (event: Event) => { if (event.target === textarea) { ime.blur(); preview('') } }
  const onPaste = (event: Event) => { if (event.target === textarea) ime.paste() }
  for (const type of OWNED_EVENTS) host.addEventListener(type, owned, true)
  host.addEventListener('blur', onBlur, true)
  host.addEventListener('paste', onPaste, true)
  return {
    reset: () => { ime.reset(); preview('') },
    dispose() {
      for (const type of OWNED_EVENTS) host.removeEventListener(type, owned, true)
      host.removeEventListener('blur', onBlur, true)
      host.removeEventListener('paste', onPaste, true)
      term.attachCustomKeyEventHandler(() => true)
    },
  }
}
