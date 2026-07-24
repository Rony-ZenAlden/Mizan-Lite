// Bindings wrapper. Every Go call goes through a wrapper like this so the UI has one
// place to unwrap results and one error path. In the desktop webview, Wails injects
// window.go.main.App.*; in a plain browser it is absent, so we fall back to a mock.

export interface HealthInfo {
  version: string;
  commit: string;
  buildTime: string;
  goVersion: string;
  platform: string;
}

interface WailsBridge {
  main?: {
    App?: {
      Health?: () => Promise<HealthInfo>;
    };
  };
}

declare global {
  interface Window {
    go?: WailsBridge;
  }
}

export async function getHealth(): Promise<HealthInfo> {
  const health = window.go?.main?.App?.Health;
  if (health) {
    return health();
  }
  // Browser-dev fallback (no Wails bridge present).
  return {
    version: "dev (browser)",
    commit: "unknown",
    buildTime: "unknown",
    goVersion: "unknown",
    platform: "browser",
  };
}
