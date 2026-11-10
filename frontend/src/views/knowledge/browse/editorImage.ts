import { Image } from '@tiptap/extension-image'
import { mergeAttributes } from '@tiptap/core'
import { NodeSelection } from '@tiptap/pm/state'
import { closeHistory } from '@tiptap/pm/history'
import { imageAlignment, imageDimension, imageLayoutStyle, parseImageAlignment, parseImageDimension } from './imageLayout'

/** Keep authenticated display URLs out of the persisted Markdown source. */
export function createEditorImage(hydrate: (root: HTMLElement) => void, labels = { resize: 'Resize image (arrow keys adjust, Home resets)' }) {
  return Image.extend({
    addAttributes() {
      return {
        ...this.parent!(),
        width: { default: null, parseHTML: element => parseImageDimension(element, 'width') },
        height: { default: null, parseHTML: element => parseImageDimension(element, 'height') },
        align: { default: null, parseHTML: parseImageAlignment, rendered: false },
      }
    },
    renderHTML({ node, HTMLAttributes }) {
      return ['img', mergeAttributes(this.options.HTMLAttributes, HTMLAttributes, {
        'data-image-align': imageAlignment(node.attrs.align), style: imageLayoutStyle(node.attrs),
      })]
    },
    addNodeView() {
      return ({ node, view, getPos }) => {
        let current = node
        const dom = document.createElement('div')
        dom.className = 'editor-image'
        dom.contentEditable = 'false'
        const frame = document.createElement('span')
        frame.className = 'editor-image__frame'
        const img = document.createElement('img')
        const handle = document.createElement('button')
        handle.type = 'button'; handle.className = 'editor-image__resize'
        handle.setAttribute('aria-label', labels.resize); handle.title = labels.resize
        handle.hidden = true
        dom.append(frame); frame.append(img, handle)
        let drag: { pointer: number; x: number; width: number; direction: number } | null = null
        let preview = 0
        const renderLayout = () => {
          dom.dataset.align = imageAlignment(current.attrs.align)
          const width = imageDimension(current.attrs.width)
          frame.style.width = width ? `${width}px` : 'fit-content'
          img.style.width = width ? '100%' : ''
          img.style.height = 'auto'
          img.style.maxWidth = '100%'
        }
        const renderSource = () => {
          img.removeAttribute('data-protected-src')
          img.removeAttribute('data-auth-hydrated')
          img.setAttribute('src', current.attrs.src || '')
          img.setAttribute('alt', current.attrs.alt || '')
          if (current.attrs.title) img.setAttribute('title', current.attrs.title)
          else img.removeAttribute('title')
          hydrate(dom)
        }
        const cancel = () => { drag = null; dom.classList.remove('is-resizing'); renderLayout() }
        const commit = (width: number | null) => {
          const pos = getPos()
          if (!view.editable || pos === undefined || view.state.doc.nodeAt(pos) !== current) return
          if (current.attrs.width === width && current.attrs.height == null) return
          const tr = view.state.tr.setNodeMarkup(pos, undefined, { ...current.attrs, width, height: null })
          tr.setSelection(NodeSelection.create(tr.doc, pos))
          view.dispatch(closeHistory(tr))
          view.dispatch(closeHistory(view.state.tr))
        }
        const clamp = (width: number) => Math.round(Math.max(1, Math.min(4096,
          dom.getBoundingClientRect().width || dom.parentElement?.clientWidth || 4096, width)))
        const start = (event: PointerEvent) => {
          if (!view.editable || event.button !== 0 || drag) return
          event.preventDefault(); event.stopPropagation()
          handle.focus(); handle.setPointerCapture(event.pointerId)
          const align = imageAlignment(current.attrs.align)
          drag = { pointer: event.pointerId, x: event.clientX, width: img.getBoundingClientRect().width,
            direction: align === 'right' ? -1 : align === 'center' ? 2 : 1 }
          preview = drag.width
          dom.classList.add('is-resizing')
        }
        const move = (event: PointerEvent) => {
          if (!drag || drag.pointer !== event.pointerId) return
          preview = clamp(drag.width + (event.clientX - drag.x) * drag.direction)
          frame.style.width = `${preview}px`; img.style.width = '100%'
        }
        const finish = (event: PointerEvent) => {
          if (!drag || drag.pointer !== event.pointerId) return
          const width = preview
          if (width === drag.width) { cancel(); return }
          cancel(); commit(width)
        }
        const abort = (event: PointerEvent) => { if (drag?.pointer === event.pointerId) cancel() }
        const keyboard = (event: KeyboardEvent) => {
          if (event.key === 'Escape' && drag) { event.preventDefault(); cancel(); return }
          if (!view.editable || !['ArrowLeft', 'ArrowRight', 'Home'].includes(event.key)) return
          event.preventDefault(); event.stopPropagation(); cancel()
          commit(event.key === 'Home' ? null : clamp(img.getBoundingClientRect().width + (event.key === 'ArrowLeft' ? -16 : 16)))
        }
        handle.addEventListener('pointerdown', start)
        handle.addEventListener('pointermove', move)
        handle.addEventListener('pointerup', finish)
        handle.addEventListener('pointercancel', abort)
        handle.addEventListener('lostpointercapture', abort)
        handle.addEventListener('keydown', keyboard)
        renderSource(); renderLayout()
        return {
          dom,
          update(next) {
            if (next.type !== current.type) return false
            const changed = next.attrs.src !== current.attrs.src || next.attrs.alt !== current.attrs.alt || next.attrs.title !== current.attrs.title
            if (drag && (changed || next.attrs.width !== current.attrs.width || next.attrs.height !== current.attrs.height || next.attrs.align !== current.attrs.align)) cancel()
            current = next
            if (changed) renderSource()
            if (!drag) renderLayout()
            return true
          },
          selectNode() { img.classList.add('ProseMirror-selectednode'); handle.hidden = !view.editable },
          deselectNode() { img.classList.remove('ProseMirror-selectednode'); handle.hidden = true; cancel() },
          stopEvent: event => event.target === handle,
          ignoreMutation: mutation => mutation.type !== 'selection',
          destroy() {
            drag = null
            handle.removeEventListener('pointerdown', start); handle.removeEventListener('pointermove', move)
            handle.removeEventListener('pointerup', finish); handle.removeEventListener('pointercancel', abort)
            handle.removeEventListener('lostpointercapture', abort); handle.removeEventListener('keydown', keyboard)
          },
        }
      }
    },
  })
}
