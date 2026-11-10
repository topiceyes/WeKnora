import { DialogPlugin } from 'tdesign-vue-next'

export function confirmDiscardChanges(t: (key: string) => string): Promise<boolean> {
  return new Promise(resolve => {
    const finish = (discard: boolean) => {
      resolve(discard)
      dialog.destroy()
    }
    const dialog = DialogPlugin.confirm({
      header: t('documentWorkspace.unsavedTitle'),
      body: t('documentWorkspace.unsavedBody'),
      confirmBtn: t('documentWorkspace.discard'),
      cancelBtn: t('documentWorkspace.keepEditing'),
      closeOnOverlayClick: false,
      onConfirm: () => finish(true),
      onCancel: () => finish(false),
      onClose: () => finish(false),
    })
  })
}
