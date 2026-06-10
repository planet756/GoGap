import { FUND_CATEGORIES, type DashboardState, type FundCategory, type SnapshotResponse, type SortDirection, type SortKey } from './types';

export const dashboardStateStorageKey = 'gogap.dashboardState';

export const createInitialState = (): DashboardState => ({
  selectedCategory: null,
  searchQuery: '',
  sortKey: null,
  sortDirection: 'desc',
  watchlistOnly: false,
  purchaseLimitOnly: false,
  items: [],
  source: 'loading',
  stale: 'unknown',
  errors: [],
  progress: null,
});

export const state: DashboardState = createInitialState();

type DashboardPreferences = Pick<DashboardState, 'selectedCategory' | 'searchQuery' | 'sortKey' | 'sortDirection' | 'watchlistOnly' | 'purchaseLimitOnly'>;

const dashboardPreferences = (): DashboardPreferences => ({
  selectedCategory: state.selectedCategory,
  searchQuery: state.searchQuery,
  sortKey: state.sortKey,
  sortDirection: state.sortDirection,
  watchlistOnly: state.watchlistOnly,
  purchaseLimitOnly: state.purchaseLimitOnly,
});

export const hydrateDashboardPreferences = (): DashboardState => {
  const raw = globalThis.localStorage?.getItem(dashboardStateStorageKey);
  if (!raw) {
    return state;
  }
  try {
    const preferences = JSON.parse(raw) as Partial<DashboardPreferences>;
    if (preferences.selectedCategory === null || FUND_CATEGORIES.some((category) => category === preferences.selectedCategory)) {
      state.selectedCategory = preferences.selectedCategory ?? null;
    }
    if (typeof preferences.searchQuery === 'string') {
      state.searchQuery = preferences.searchQuery;
    }
    if (preferences.sortKey === null || preferences.sortKey === 'premiumRate' || preferences.sortKey === 'quoteChangePercent' || preferences.sortKey === 'navChangePercent' || preferences.sortKey === 'turnoverAmount') {
      state.sortKey = preferences.sortKey ?? null;
    }
    if (preferences.sortDirection === 'asc' || preferences.sortDirection === 'desc') {
      state.sortDirection = preferences.sortDirection;
    }
    if (typeof preferences.watchlistOnly === 'boolean') {
      state.watchlistOnly = preferences.watchlistOnly;
    }
    if (typeof preferences.purchaseLimitOnly === 'boolean') {
      state.purchaseLimitOnly = preferences.purchaseLimitOnly;
    }
  } catch {
    return state;
  }
  return state;
};

export const persistDashboardPreferences = (): void => {
  globalThis.localStorage?.setItem(dashboardStateStorageKey, JSON.stringify(dashboardPreferences()));
};

export const resetState = (): DashboardState => {
  Object.assign(state, createInitialState());
  return state;
};

export const selectCategory = (category: FundCategory | null): DashboardState => {
  state.selectedCategory = category;
  persistDashboardPreferences();
  return state;
};

export const setSearchQuery = (searchQuery: string): DashboardState => {
  state.searchQuery = searchQuery;
  persistDashboardPreferences();
  return state;
};

export const setSort = (sortKey: SortKey | null, sortDirection: SortDirection = state.sortDirection): DashboardState => {
  state.sortKey = sortKey;
  state.sortDirection = sortDirection;
  persistDashboardPreferences();
  return state;
};

export const setWatchlistOnly = (watchlistOnly: boolean): DashboardState => {
  state.watchlistOnly = watchlistOnly;
  persistDashboardPreferences();
  return state;
};

export const setPurchaseLimitOnly = (purchaseLimitOnly: boolean): DashboardState => {
  state.purchaseLimitOnly = purchaseLimitOnly;
  persistDashboardPreferences();
  return state;
};

export const clearDashboardFilters = (): DashboardState => {
  state.selectedCategory = null;
  state.searchQuery = '';
  state.sortKey = null;
  state.sortDirection = 'desc';
  state.watchlistOnly = false;
  state.purchaseLimitOnly = false;
  persistDashboardPreferences();
  return state;
};

export const updateFundWatchlist = (code: string, inWatchlist: boolean): DashboardState => {
  state.items = state.items.map((item) => (item.code === code ? { ...item, inWatchlist } : item));
  return state;
};

export const applySnapshot = (snapshot: SnapshotResponse): DashboardState => {
  state.items = snapshot.items;
  state.source = snapshot.source;
  state.stale = snapshot.stale;
  state.errors = snapshot.errors;
  state.progress = snapshot.progress;
  return state;
};

export const applyStreamSnapshot = (snapshot: SnapshotResponse): DashboardState => {
  const watchlistByCode = new Map(state.items.map((item) => [item.code, item.inWatchlist]));
  return applySnapshot({
    ...snapshot,
    items: snapshot.items.map((item) => ({ ...item, inWatchlist: watchlistByCode.get(item.code) ?? item.inWatchlist })),
  });
};

export const applySnapshotError = (message: string): DashboardState => {
  state.source = 'error';
  state.errors = [message];
  state.progress = null;
  return state;
};
