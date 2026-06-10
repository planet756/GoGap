import type { SnapshotResponse } from './types';

export const snapshotEndpoint = '/api/snapshot';
export const eventsEndpoint = '/events';
const visitorStorageKey = 'gogap.visitorId';

export type FetchSnapshot = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;

export interface SnapshotEventSource {
  addEventListener(type: string, listener: (event: Event) => void): void;
  removeEventListener(type: string, listener: (event: Event) => void): void;
  close(): void;
}

export type SnapshotEventSourceConstructor = new (url: string) => SnapshotEventSource;

export interface SnapshotStreamHandlers {
  onOpen?: () => void;
  onError?: () => void;
  onSnapshot: (snapshot: SnapshotResponse) => void;
}

export interface SnapshotStreamOptions {
  eventSource?: SnapshotEventSourceConstructor;
}

export const createEmptySnapshot = (): SnapshotResponse => ({
  disclaimer: '数据来源于公开信息；交易时段折溢价可基于盘中估值与最新场内价格计算，非交易时段基于官方已披露净值；不构成投资建议；投资有风险。',
  items: [],
  source: 'partial',
  stale: 'stale',
  errors: [],
  progress: { label: '拉取数据', percent: 0 },
});

export const visitorID = (): string => {
  const existing = globalThis.localStorage?.getItem(visitorStorageKey);
  if (existing) {
    return existing;
  }
  const generated = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  globalThis.localStorage?.setItem(visitorStorageKey, generated);
  return generated;
};

const visitorHeaders = (): Record<string, string> => ({ 'X-GoGap-Visitor-Id': visitorID() });

export const loadInitialSnapshot = async (fetchSnapshot: FetchSnapshot = globalThis.fetch.bind(globalThis)): Promise<SnapshotResponse> => {
  const response = await fetchSnapshot(snapshotEndpoint, {
    headers: {
      Accept: 'application/json',
      ...visitorHeaders(),
    },
  });

  if (!response.ok) {
    throw new Error(`Snapshot request failed with ${response.status}`);
  }

  return (await response.json()) as SnapshotResponse;
};

export const refreshSnapshot = async (fetchSnapshot: FetchSnapshot = globalThis.fetch.bind(globalThis)): Promise<SnapshotResponse> => {
  const response = await fetchSnapshot(snapshotEndpoint, {
    method: 'POST',
    headers: {
      Accept: 'application/json',
      ...visitorHeaders(),
    },
  });

  if (!response.ok) {
    throw new Error(`Snapshot refresh failed with ${response.status}`);
  }

  return (await response.json()) as SnapshotResponse;
};

export const addWatchlistFund = async (code: string, fetchWatchlist: FetchSnapshot = globalThis.fetch.bind(globalThis)): Promise<void> => {
  const response = await fetchWatchlist('/api/watchlist', {
    method: 'POST',
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
      ...visitorHeaders(),
    },
    body: JSON.stringify({ code }),
  });

  if (!response.ok) {
    throw new Error(`Watchlist add failed with ${response.status}`);
  }
};

export const removeWatchlistFund = async (code: string, fetchWatchlist: FetchSnapshot = globalThis.fetch.bind(globalThis)): Promise<void> => {
  const response = await fetchWatchlist(`/api/watchlist/${encodeURIComponent(code)}`, {
    method: 'DELETE',
    headers: {
      Accept: 'application/json',
      ...visitorHeaders(),
    },
  });

  if (!response.ok) {
    throw new Error(`Watchlist remove failed with ${response.status}`);
  }
};

// SSE reconnection with exponential backoff
const SSE_INITIAL_RETRY_MS = 1000;
const SSE_MAX_RETRY_MS = 30000;

export const connectSnapshotStream = (
  handlers: SnapshotStreamHandlers,
  options: SnapshotStreamOptions = {},
): SnapshotEventSource => {
  const EventSourceConstructor = options.eventSource ?? globalThis.EventSource;
  let retryDelay = SSE_INITIAL_RETRY_MS;
  let retryTimer: ReturnType<typeof setTimeout> | null = null;
  let closed = false;

  let currentStream: SnapshotEventSource;

  const connect = (): SnapshotEventSource => {
    const stream = new EventSourceConstructor(eventsEndpoint) as SnapshotEventSource;

    stream.addEventListener('open', () => {
      retryDelay = SSE_INITIAL_RETRY_MS;
      handlers.onOpen?.();
    });
    stream.addEventListener('error', () => {
      handlers.onError?.();
      if (!closed) {
        stream.close();
        retryTimer = setTimeout(() => {
          retryTimer = null;
          if (!closed) {
            currentStream = connect();
          }
        }, retryDelay);
        retryDelay = Math.min(retryDelay * 2, SSE_MAX_RETRY_MS);
      }
    });
    stream.addEventListener('snapshot', (event) => {
      handlers.onSnapshot(JSON.parse((event as MessageEvent<string>).data) as SnapshotResponse);
    });

    return stream;
  };

  currentStream = connect();

  // Return a wrapper that supports close()
  return {
    addEventListener: (type: string, listener: (event: Event) => void) => currentStream.addEventListener(type, listener),
    removeEventListener: (type: string, listener: (event: Event) => void) => currentStream.removeEventListener(type, listener),
    close: () => {
      closed = true;
      if (retryTimer !== null) {
        clearTimeout(retryTimer);
        retryTimer = null;
      }
      currentStream.close();
    },
  };
};
