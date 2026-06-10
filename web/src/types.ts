export const FUND_CATEGORIES = ['QDII', '商品', '香港', '主动LOF', '指数LOF', 'ETF', '债券货币'] as const;

export type FundCategory = (typeof FUND_CATEGORIES)[number];

export type SourceState = 'ready' | 'partial' | 'error';

export type StaleState = 'fresh' | 'stale' | 'closed';

export type SortKey = 'premiumRate' | 'quoteChangePercent' | 'navChangePercent' | 'turnoverAmount';

export type SortDirection = 'asc' | 'desc';

export const SORT_LABELS: Record<SortKey, string> = {
  premiumRate: '溢价率',
  quoteChangePercent: '场内涨跌幅',
  navChangePercent: '净值涨跌幅',
  turnoverAmount: '成交额',
};

export interface FundSnapshot {
  code: string;
  name: string;
  category: FundCategory;
  nav: number | null;
  navDate: string;
  navBasis: string;
  navChangePercent: number | null;
  quotePrice: number | null;
  quoteChangePercent: number | null;
  quoteTime: string;
  premiumRate: number | null;
  purchaseStatus: string;
  redemptionStatus: string;
  purchaseLimit: string;
  fundScale: string;
  fundScaleDate: string;
  turnoverRate: number | null;
  turnoverAmount: number | null;
  turnoverAmountUnit: string;
  inWatchlist: boolean;
  source: SourceState;
  stale: StaleState;
  errors: string[];
}

export interface SnapshotResponse {
  disclaimer: string;
  items: FundSnapshot[];
  source: SourceState;
  stale: StaleState;
  errors: string[];
  progress: ProgressState | null;
}

export interface ProgressState {
  label: string;
  percent: number;
}

export interface DashboardState {
  selectedCategory: FundCategory | null;
  searchQuery: string;
  sortKey: SortKey | null;
  sortDirection: SortDirection;
  watchlistOnly: boolean;
  purchaseLimitOnly: boolean;
  items: FundSnapshot[];
  source: SourceState | 'loading';
  stale: StaleState | 'unknown';
  errors: string[];
  progress: ProgressState | null;
}
