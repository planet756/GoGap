import { addWatchlistFund, connectSnapshotStream, loadInitialSnapshot, refreshSnapshot, removeWatchlistFund, type FetchSnapshot, type SnapshotEventSource, type SnapshotEventSourceConstructor } from './api';
import { renderDashboard, mountDashboardShell } from './render';
import {
  applySnapshot,
  applyStreamSnapshot,
  applySnapshotError,
  selectCategory,
  setSearchQuery,
  setPurchaseLimitOnly,
  setSort,
  setWatchlistOnly,
  state,
  hydrateDashboardPreferences,
  clearDashboardFilters,
  updateFundWatchlist,
} from './state';
import { FUND_CATEGORIES, type FundCategory, type SortDirection, type SortKey } from './types';

function debounce<T extends (...args: never[]) => void>(fn: T, delay: number): T {
  let timer: ReturnType<typeof setTimeout> | undefined;
  return ((...args: Parameters<T>) => {
    if (timer !== undefined) {
      clearTimeout(timer);
    }
    timer = setTimeout(() => {
      timer = undefined;
      fn(...args);
    }, delay);
  }) as unknown as T;
}

export interface DashboardOptions {
  eventSource?: SnapshotEventSourceConstructor;
  fetchSnapshot?: FetchSnapshot;
  fetchWatchlist?: FetchSnapshot;
}

let activeCleanup: (() => void) | null = null;
let activeStream: SnapshotEventSource | null = null;

export const initializeDashboard = (root: HTMLElement, options: DashboardOptions = {}): void => {
  teardownDashboard();
  hydrateDashboardPreferences();
  mountDashboardShell(root, state);
  activeCleanup = bindDashboardControls(options.fetchWatchlist, options.fetchSnapshot);
  activeStream = connectSnapshotUpdates(options.eventSource);

  void refreshDashboardSnapshot(options.fetchSnapshot);
};

export const teardownDashboard = (): void => {
  activeCleanup?.();
  activeCleanup = null;
  activeStream?.close();
  activeStream = null;
};

const dashboardRoot = document.querySelector<HTMLElement>('[data-dashboard-shell]') ?? document.querySelector<HTMLElement>('#app');

if (dashboardRoot) {
  initializeDashboard(dashboardRoot);
}

function bindCategoryFilters(): Array<() => void> {
  const cleanups = Array.from(document.querySelectorAll<HTMLButtonElement>('[data-category]')).map((chip) => {
    const onClick = () => {
      const category = chip.dataset.category;
      if (!isFundCategory(category)) {
        return;
      }

      selectCategory(state.selectedCategory === category ? null : category);
      renderDashboard(state);
    };

    chip.addEventListener('click', onClick);
    return () => chip.removeEventListener('click', onClick);
  });

  const allCategory = document.querySelector<HTMLButtonElement>('[data-category-all]');
  const onAllClick = (): void => {
    selectCategory(null);
    renderDashboard(state);
  };
  allCategory?.addEventListener('click', onAllClick);
  if (allCategory) {
    cleanups.push(() => allCategory.removeEventListener('click', onAllClick));
  }
  return cleanups;
}

function bindDashboardControls(fetchWatchlist?: FetchSnapshot, fetchSnapshot?: FetchSnapshot): () => void {
  const cleanups = bindCategoryFilters();

  const searchInput = document.querySelector<HTMLInputElement>('[data-search-input]');
  const onSearchInput = debounce((event: Event): void => {
    setSearchQuery((event.currentTarget as HTMLInputElement).value);
    renderDashboard(state);
  }, 200);
  const onSearchKeydown = (event: KeyboardEvent): void => {
    if (event.key === 'Enter') {
      event.preventDefault();
      searchInput?.blur();
    }
  };
  searchInput?.addEventListener('input', onSearchInput);
  searchInput?.addEventListener('keydown', onSearchKeydown);
  if (searchInput) {
    cleanups.push(() => searchInput.removeEventListener('input', onSearchInput));
    cleanups.push(() => searchInput.removeEventListener('keydown', onSearchKeydown));
  }

  const searchButton = document.querySelector<HTMLButtonElement>('[data-search-submit]');
  const onSearchClick = (): void => {
    setSearchQuery(searchInput?.value ?? '');
    renderDashboard(state);
    searchInput?.blur();
  };
  searchButton?.addEventListener('click', onSearchClick);
  if (searchButton) {
    cleanups.push(() => searchButton.removeEventListener('click', onSearchClick));
  }

  const sortSelect = document.querySelector<HTMLSelectElement>('[data-sort-select]');
  const onSortChange = (event: Event): void => {
    const value = (event.currentTarget as HTMLSelectElement).value;
    const [sortKey, sortDirection] = value.split(':');
    if (value === '' || !isSortKey(sortKey) || !isSortDirection(sortDirection)) {
      setSort(null, 'desc');
    } else {
      setSort(sortKey, sortDirection);
    }
    renderDashboard(state);
  };
  sortSelect?.addEventListener('change', onSortChange);
  if (sortSelect) {
    cleanups.push(() => sortSelect.removeEventListener('change', onSortChange));
  }

  const watchlistOnly = document.querySelector<HTMLButtonElement>('[data-watchlist-only]');
  const onWatchlistOnlyClick = (): void => {
    setWatchlistOnly(!state.watchlistOnly);
    renderDashboard(state);
  };
  watchlistOnly?.addEventListener('click', onWatchlistOnlyClick);
  if (watchlistOnly) {
    cleanups.push(() => watchlistOnly.removeEventListener('click', onWatchlistOnlyClick));
  }

  const purchaseLimitOnly = document.querySelector<HTMLButtonElement>('[data-purchase-limit-only]');
  const onPurchaseLimitOnlyClick = (): void => {
    setPurchaseLimitOnly(!state.purchaseLimitOnly);
    renderDashboard(state);
  };
  purchaseLimitOnly?.addEventListener('click', onPurchaseLimitOnlyClick);
  if (purchaseLimitOnly) {
    cleanups.push(() => purchaseLimitOnly.removeEventListener('click', onPurchaseLimitOnlyClick));
  }

  const clearFilters = document.querySelector<HTMLButtonElement>('[data-filter-clear]');
  const onClearFiltersClick = (): void => {
    clearDashboardFilters();
    renderDashboard(state);
  };
  clearFilters?.addEventListener('click', onClearFiltersClick);
  if (clearFilters) {
    cleanups.push(() => clearFilters.removeEventListener('click', onClearFiltersClick));
  }

  const sortHeaderButtons = Array.from(document.querySelectorAll<HTMLButtonElement>('[data-sort-button]')).map((button) => {
    const onClick = (): void => {
      const key = button.dataset.sortKey;
      if (!isSortKey(key)) {
        return;
      }
      const direction = state.sortKey === key && state.sortDirection === 'desc' ? 'asc' : 'desc';
      setSort(key, direction);
      renderDashboard(state);
    };
    button.addEventListener('click', onClick);
    return () => button.removeEventListener('click', onClick);
  });
  cleanups.push(...sortHeaderButtons);

  const refreshButton = document.querySelector<HTMLButtonElement>('[data-snapshot-refresh]');
  const onRefreshClick = (): void => {
    void refreshDashboardSnapshot(fetchSnapshot, refreshButton, true);
  };
  refreshButton?.addEventListener('click', onRefreshClick);
  if (refreshButton) {
    cleanups.push(() => refreshButton.removeEventListener('click', onRefreshClick));
  }

  const shell = document.querySelector('[data-dashboard-shell]');
  const onShellClick = (event: Event): void => {
    const button = (event.target as HTMLElement).closest<HTMLButtonElement>('[data-watchlist-action]');
    if (!button) {
      return;
    }

    const code = button.dataset.code;
    if (!code) {
      return;
    }

    void handleWatchlistAction(code, button.dataset.inWatchlist === 'true', fetchWatchlist);
  };
  shell?.addEventListener('click', onShellClick);
  if (shell) {
    cleanups.push(() => shell.removeEventListener('click', onShellClick));
  }

  return () => cleanups.forEach((cleanup) => cleanup());
}

async function refreshDashboardSnapshot(fetchSnapshot?: FetchSnapshot, trigger?: HTMLButtonElement | null, forceRefresh = false): Promise<void> {
  if (trigger) {
    trigger.disabled = true;
  }

  try {
    const snapshot = forceRefresh ? await refreshSnapshot(fetchSnapshot) : await loadInitialSnapshot(fetchSnapshot);
    applySnapshot(snapshot);
    renderDashboard(state);
    if (trigger) {
      trigger.textContent = '已刷新';
      window.setTimeout(() => {
        trigger.textContent = '刷新';
      }, 1200);
    }
  } catch (error: unknown) {
    applySnapshotError(error instanceof Error ? error.message : 'Snapshot request failed');
    renderDashboard(state);
    if (trigger) {
      trigger.textContent = '失败';
      window.setTimeout(() => {
        trigger.textContent = '刷新';
      }, 1600);
    }
  } finally {
    if (trigger) {
      trigger.disabled = false;
    }
  }
}

async function handleWatchlistAction(code: string, shouldRemove: boolean, fetchWatchlist?: FetchSnapshot): Promise<void> {
  document.querySelector<HTMLButtonElement>(`[data-watchlist-action][data-code="${code}"]`)?.setAttribute('disabled', '');
  const previousWatchlistState = shouldRemove;
  updateFundWatchlist(code, !shouldRemove);
  renderDashboard(state);
  try {
    if (shouldRemove) {
      await removeWatchlistFund(code, fetchWatchlist);
    } else {
      await addWatchlistFund(code, fetchWatchlist);
    }
  } catch (error: unknown) {
    updateFundWatchlist(code, previousWatchlistState);
    renderDashboard(state);
    const button = document.querySelector<HTMLButtonElement>(`[data-watchlist-action][data-code="${code}"]`);
    if (button) {
      button.disabled = false;
      button.setAttribute('aria-label', error instanceof Error ? error.message : '自选更新失败');
    }
  }
}

function connectSnapshotUpdates(eventSource?: SnapshotEventSourceConstructor): SnapshotEventSource {
  return connectSnapshotStream({
    onSnapshot: (snapshot) => {
      applyStreamSnapshot(snapshot);
      renderDashboard(state);
    },
  }, eventSource ? { eventSource } : {});
}

function isFundCategory(value: string | undefined): value is FundCategory {
  return FUND_CATEGORIES.some((category) => category === value);
}

function isSortKey(value: string | undefined): value is SortKey {
  return value === 'premiumRate' || value === 'quoteChangePercent' || value === 'navChangePercent' || value === 'turnoverAmount';
}

function isSortDirection(value: string | undefined): value is SortDirection {
  return value === 'asc' || value === 'desc';
}
