export async function openVOSApp(appId: string, pageId: string, fallbackUrl: string): Promise<void> {
  const openTab = window.vos_platform?.platform?.openTab
  if (!openTab) {
    window.location.assign(fallbackUrl)
    return
  }

  try {
    await openTab({ app: { id: appId, page: pageId } })
  } catch (error) {
    const name = (error as { name?: string } | null)?.name
    if (name !== 'PlatformNotAuthenticatedError') {
      window.location.assign(fallbackUrl)
    }
  }
}
