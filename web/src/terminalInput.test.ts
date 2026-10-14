import { describe, expect, it } from 'vitest'
import { createImeController, editFor } from './terminalInput'

const DEL = '\x7f'
const IME = 229

function controller() {
  const field = { value: '' }
  const sent: string[] = []
  const ime = createImeController({ field, emit: text => sent.push(text) })
  // keyCode 229 keydown, an IME edit of the field, then the input event.
  const edit = (next: string, keydowns = 1) => {
    for (let i = 0; i < keydowns; i++) expect(ime.keydown({ keyCode: IME, key: 'Unidentified', ctrlKey: false })).toBe(false)
    field.value = next
    ime.input()
    ime.keyup()
  }
  return { field, sent, ime, edit }
}

describe('editFor', () => {
  it('appends new text', () => {
    expect(editFor('ab', 'ab ')).toBe(' ')
    expect(editFor('', 'go')).toBe('go')
  })
  it('deletes back to the first difference and retypes the rest', () => {
    expect(editFor('ab', 'ac')).toBe(`${DEL}c`)
    expect(editFor('teh ', 'the ')).toBe(`${DEL}${DEL}${DEL}he `)
    expect(editFor('abc', 'xabc')).toBe(`${DEL}${DEL}${DEL}xabc`)
  })
  it('sends nothing for an unchanged value', () => {
    expect(editFor('같다', '같다')).toBe('')
  })
  it('counts code points, not UTF-16 units', () => {
    expect(editFor('a😀', 'a')).toBe(DEL)
    expect(editFor('a😀b', 'a😀c')).toBe(`${DEL}c`)
  })
})

describe('IME controller', () => {
  it('sends only the edit when the IME rewrites text in place after keyCode 229', () => {
    const { sent, edit } = controller()
    edit('ab')
    edit('ac')
    expect(sent).toEqual(['ab', `${DEL}c`])
  })
  it('sends an append once even when two 229 keydowns precede it', () => {
    const { sent, edit } = controller()
    edit('ab')
    edit('abgo', 2)
    expect(sent).toEqual(['ab', 'go'])
  })
  it('sends a composed syllable once, also when the IME recomposes over it', () => {
    const { field, sent, ime } = controller()
    ime.keydown({ keyCode: IME, key: 'Unidentified', ctrlKey: false })
    ime.compositionstart()
    for (const value of ['ㅌ', '토', '통']) { field.value = value; ime.input() }
    ime.compositionend()
    ime.keyup()
    expect(sent).toEqual(['통'])
    ime.keydown({ keyCode: IME, key: 'Unidentified', ctrlKey: false })
    ime.compositionstart()
    for (const value of ['톹', '통토', '통통']) { field.value = value; ime.input() }
    ime.compositionend()
    // Safari reports the committed text with one more input event after compositionend.
    ime.input()
    ime.keyup()
    expect(sent).toEqual(['통', '통'])
  })
  it('does not resend a character xterm already sent from a real keydown', () => {
    const { field, sent, ime, edit } = controller()
    expect(ime.keydown({ keyCode: 65, key: 'a', ctrlKey: false })).toBe(true)
    field.value = 'a'
    ime.input()
    ime.keyup()
    expect(sent).toEqual([])
    edit('ab')
    expect(sent).toEqual(['b'])
  })
  it('flushes a composition before Enter and starts over afterwards', () => {
    const { field, sent, ime } = controller()
    ime.keydown({ keyCode: IME, key: 'Unidentified', ctrlKey: false })
    ime.compositionstart()
    field.value = '가'
    ime.input()
    ime.keyup()
    expect(sent).toEqual([])
    expect(ime.keydown({ keyCode: 13, key: 'Enter', ctrlKey: false })).toBe(true)
    expect(sent).toEqual(['가'])
    // xterm sends CR and empties the textarea on its own Enter keydown.
    field.value = ''
    ime.compositionend()
    ime.keyup()
    field.value = '나'
    ime.keydown({ keyCode: IME, key: 'Unidentified', ctrlKey: false })
    ime.input()
    expect(sent).toEqual(['가', '나'])
  })
  it('sends a composition before a real key that xterm handles next', () => {
    const { field, sent, ime } = controller()
    ime.keydown({ keyCode: IME, key: 'Unidentified', ctrlKey: false })
    ime.compositionstart()
    field.value = '가'
    ime.input()
    ime.keyup()
    expect(ime.keydown({ keyCode: 16, key: 'Shift', ctrlKey: false })).toBe(true)
    expect(sent).toEqual([])
    expect(ime.keydown({ keyCode: 32, key: ' ', ctrlKey: false })).toBe(true)
    expect(sent).toEqual(['가'])
    ime.compositionend()
    ime.keyup()
    expect(sent).toEqual(['가'])
  })
  it('treats Ctrl-C like Enter', () => {
    const { field, sent, ime, edit } = controller()
    edit('ab')
    expect(ime.keydown({ keyCode: 67, key: 'c', ctrlKey: true })).toBe(true)
    field.value = ''
    ime.keyup()
    edit('c')
    expect(sent).toEqual(['ab', 'c'])
  })
  it('sends a backspace when the IME deletes from an empty field', () => {
    const { sent, ime } = controller()
    expect(ime.beforeinput('deleteContentBackward')).toBe(true)
    expect(sent).toEqual([DEL])
    expect(ime.beforeinput('insertText')).toBe(false)
  })
  it('turns an inserted line break into CR and empties the field', () => {
    const { field, sent, edit } = controller()
    edit('ab')
    edit('ab\n')
    expect(sent).toEqual(['ab', '\r'])
    expect(field.value).toBe('')
    edit('c')
    expect(sent).toEqual(['ab', '\r', 'c'])
  })
  it('starts over after blur, paste and reset instead of resending old text', () => {
    const { field, sent, ime, edit } = controller()
    edit('ab')
    ime.blur()
    field.value = ''
    edit('c')
    expect(sent).toEqual(['ab', 'c'])
    ime.paste()
    field.value = ''
    edit('d')
    expect(sent).toEqual(['ab', 'c', 'd'])
    ime.reset()
    expect(field.value).toBe('')
    edit('e')
    expect(sent).toEqual(['ab', 'c', 'd', 'e'])
  })
  it('lets Backspace edit a composition in the browser and sends the net result', () => {
    const { field, sent, ime, edit } = controller()
    edit('가')
    ime.keydown({ keyCode: IME, key: 'Unidentified', ctrlKey: false })
    ime.compositionstart()
    field.value = '가안'
    ime.input()
    ime.keyup()
    expect(ime.keydown({ keyCode: 8, key: 'Backspace', ctrlKey: false })).toBe(false)
    field.value = '가아'
    ime.input()
    ime.keyup()
    expect(sent).toEqual(['가'])
    ime.compositionend()
    expect(sent).toEqual(['가', '아'])
  })
  it('does not add to a Backspace that xterm handled outside a composition', () => {
    const { field, sent, ime, edit } = controller()
    edit('ab')
    expect(ime.keydown({ keyCode: 8, key: 'Backspace', ctrlKey: false })).toBe(true)
    // Whether or not the browser also removed the character, xterm sent the DEL.
    field.value = 'a'
    ime.input()
    ime.keyup()
    edit('ax')
    expect(sent).toEqual(['ab', 'x'])
  })
  it('treats keys reported as 0, Unidentified or Process as IME input', () => {
    const { field, sent, ime } = controller()
    for (const key of [{ keyCode: 0, key: 'Unidentified' }, { keyCode: 0, key: 'Process' }]) {
      expect(ime.keydown({ ...key, ctrlKey: false })).toBe(false)
      field.value += 'x'
      ime.input()
      ime.keyup()
    }
    expect(sent).toEqual(['x', 'x'])
  })
  it('flushes a pending composition on reset so a key bar Enter keeps the last syllable', () => {
    const { field, sent, ime } = controller()
    ime.keydown({ keyCode: IME, key: 'Unidentified', ctrlKey: false })
    ime.compositionstart()
    field.value = '다'
    ime.input()
    ime.reset()
    expect(sent).toEqual(['다'])
    expect(field.value).toBe('')
  })
})
