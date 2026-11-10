import { Image } from '@tiptap/extension-image'

/** Keep authenticated display URLs out of the persisted Markdown source. */
export function createEditorImage(hydrate: (root: HTMLElement) => void) {
  return Image.extend({
    addNodeView() {
      return ({ node }) => {
        let current = node
        const dom = document.createElement('div')
        dom.className = 'editor-image'
        const img = document.createElement('img')
        dom.append(img)
        const render = () => {
          img.removeAttribute('data-protected-src')
          img.removeAttribute('data-auth-hydrated')
          img.setAttribute('src', current.attrs.src || '')
          img.setAttribute('alt', current.attrs.alt || '')
          if (current.attrs.title) img.setAttribute('title', current.attrs.title)
          else img.removeAttribute('title')
          hydrate(dom)
        }
        render()
        return {
          dom,
          update(next) {
            if (next.type !== current.type) return false
            const changed = next.attrs.src !== current.attrs.src || next.attrs.alt !== current.attrs.alt || next.attrs.title !== current.attrs.title
            current = next
            if (changed) render()
            return true
          },
          selectNode() { img.classList.add('ProseMirror-selectednode') },
          deselectNode() { img.classList.remove('ProseMirror-selectednode') },
          ignoreMutation: () => true,
        }
      }
    },
  })
}
