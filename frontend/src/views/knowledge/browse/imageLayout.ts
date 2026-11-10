export type ImageAlignment = 'left' | 'center' | 'right'

export function imageDimension(value: unknown): number | null {
  return typeof value === 'number' && Number.isInteger(value) && value >= 1 && value <= 4096 ? value : null
}

export function imageAlignment(value: unknown): ImageAlignment {
  return value === 'center' || value === 'right' ? value : 'left'
}

export function imageLayoutStyle(attrs: Record<string, unknown>): string {
  const width = imageDimension(attrs.width), align = imageAlignment(attrs.align)
  return `display:block;max-width:100%;height:auto;margin-left:${align === 'left' ? '0' : 'auto'};margin-right:${align === 'right' ? '0' : 'auto'}${width ? `;width:${width}px` : ''}`
}

export function parseImageDimension(element: HTMLElement, name: 'width' | 'height') {
  const value = element.getAttribute(name) || element.style[name]
  return /^\d+(?:px)?$/.test(value) ? imageDimension(Number(value.replace(/px$/, ''))) : null
}

export function parseImageAlignment(element: HTMLElement): ImageAlignment {
  const explicit = element.getAttribute('data-image-align') || element.getAttribute('align')
  if (explicit === 'left' || explicit === 'center' || explicit === 'right') return explicit
  if (element.style.marginLeft === 'auto') return element.style.marginRight === 'auto' ? 'center' : 'right'
  return 'left'
}
