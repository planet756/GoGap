import type { DashboardState, FundSnapshot, SortKey } from './types';

export const visibleFunds = (state: DashboardState): FundSnapshot[] => {
  const query = state.searchQuery.trim().toLocaleLowerCase('zh-CN');
  const filtered = state.items.filter((item) => {
    const matchesCategory = state.selectedCategory === null || item.category === state.selectedCategory;
    const matchesSearch = query === '' || item.code.toLocaleLowerCase('zh-CN').includes(query) || item.name.toLocaleLowerCase('zh-CN').includes(query);
    const matchesWatchlist = !state.watchlistOnly || item.inWatchlist;
    const matchesPurchaseLimit = !state.purchaseLimitOnly || item.purchaseLimit.trim() !== '';

    return matchesCategory && matchesSearch && matchesWatchlist && matchesPurchaseLimit;
  });

  if (state.sortKey === null) {
    return filtered;
  }

  const sortKey = state.sortKey;
  return [...filtered].sort((left, right) => compareNullableNumber(left, right, sortKey, state.sortDirection === 'desc'));
};

const compareNullableNumber = (left: FundSnapshot, right: FundSnapshot, key: SortKey, descending: boolean): number => {
  const leftValue = left[key];
  const rightValue = right[key];

  if (leftValue === null && rightValue === null) {
    return left.code.localeCompare(right.code, 'zh-CN');
  }
  if (leftValue === null) {
    return 1;
  }
  if (rightValue === null) {
    return -1;
  }

  const numericOrder = descending ? rightValue - leftValue : leftValue - rightValue;
  return numericOrder === 0 ? left.code.localeCompare(right.code, 'zh-CN') : numericOrder;
};
