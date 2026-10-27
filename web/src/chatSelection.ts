type SelectionLike = Pick<Selection, 'isCollapsed' | 'rangeCount' | 'toString'> & { getRangeAt(index: number): Pick<Range, 'commonAncestorContainer'> }

// chatSelection returns the text of a selection made wholly inside root, or
// null when nothing quotable is selected there.
export function chatSelection(selection: SelectionLike | null, root: Pick<Node, 'contains'> | null): string | null {
  if (!selection || !root || selection.isCollapsed || selection.rangeCount === 0) return null
  if (!root.contains(selection.getRangeAt(0).commonAncestorContainer)) return null
  const text = selection.toString()
  return text.trim() ? text : null
}
