/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_GATEWAY_TARGET: string;
  /** 是否启用前端 mock（无需后端即可跑通全流程）。'true' | 'false' */
  readonly VITE_USE_MOCK?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
