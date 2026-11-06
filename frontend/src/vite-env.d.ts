/// <reference types="vite/client" />

declare const __CONTRACT_SHA__: string

declare module 'virtual:operation-tags' {
  const operationsByTag: Record<string, string[]>
  export default operationsByTag
}
