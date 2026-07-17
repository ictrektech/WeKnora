declare module 'secure-ls' {
  export default class SecureLS {
    constructor(options?: {
      encodingType?: string
      encryptionSecret?: string
      isCompression?: boolean
      metaKey?: string
    })
    get(key: string): any
    set(key: string, value: any): void
    remove(key: string): void
  }
}
