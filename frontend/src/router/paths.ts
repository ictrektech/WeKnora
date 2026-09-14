export const LEGAL_CONTRACT_REVIEW_ROUTE = 'legalContractReview'
export const LEGAL_CONTRACT_REVIEW_DETAIL_ROUTE = 'legalContractReviewDetail'
export const LEGAL_SMART_ARCHIVE_ROUTE = 'legalSmartArchive'
export const LEGAL_ASSISTANT_ROUTE = 'legalAssistant'
export const LEGAL_ASSISTANT_HOME_ROUTE = 'legalAssistantHome'
export const LEGAL_ASSISTANT_CHAT_ROUTE = 'legalAssistantChat'

export const LEGAL_ASSISTANT_ROUTE_NAMES: ReadonlySet<string> = new Set([
  LEGAL_ASSISTANT_ROUTE,
  LEGAL_ASSISTANT_HOME_ROUTE,
  LEGAL_ASSISTANT_CHAT_ROUTE,
])

export const LEGAL_WORKSPACE_ROUTE_NAMES: ReadonlySet<string> = new Set([
  LEGAL_CONTRACT_REVIEW_ROUTE,
  LEGAL_CONTRACT_REVIEW_DETAIL_ROUTE,
  LEGAL_SMART_ARCHIVE_ROUTE,
  ...LEGAL_ASSISTANT_ROUTE_NAMES,
])

export const isLegalAssistantRouteName = (name: unknown): boolean =>
  typeof name === 'string' && LEGAL_ASSISTANT_ROUTE_NAMES.has(name)

export const isLegalWorkspaceRouteName = (name: unknown): boolean =>
  typeof name === 'string' && LEGAL_WORKSPACE_ROUTE_NAMES.has(name)
