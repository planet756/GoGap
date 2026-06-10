import {
  FUND_CATEGORIES,
  type DashboardState,
  type FundCategory,
  type FundSnapshot,
  type ProgressState,
  type SourceState,
  type SortDirection,
  type SortKey,
} from './types';
import { visibleFunds } from './filters';

type HeaderConfig = {
  label: string;
  numeric?: boolean;
  sortKey?: SortKey;
};

const tableHeaders: HeaderConfig[] = [
  { label: '基金名称' },
  { label: '申购/赎回' },
  { label: '净值/估值', numeric: true, sortKey: 'navChangePercent' },
  { label: '场内价格', numeric: true, sortKey: 'quoteChangePercent' },
  { label: '折溢价率', numeric: true, sortKey: 'premiumRate' },
  { label: '换手率/成交额', numeric: true },
  { label: '自选' },
];

export const createDashboardShell = (): HTMLElement => {
  const wrapper = document.createElement('div');
  wrapper.innerHTML = `
    <main class="app-shell" id="app" data-dashboard-shell>
      <section class="hero-panel" aria-labelledby="dashboard-title">
        <h1 id="dashboard-title" class="hero-panel__title">GoGap<span class="hero-panel__sep">/</span><span class="hero-panel__sub">基金折溢价看板</span></h1>
      </section>

      <section class="filter-panel" aria-label="基金分类筛选">
        <div class="filter-panel__controls">
          <nav class="chip-group" aria-label="基金分类筛选">
            ${allCategoryChipMarkup()}
            ${FUND_CATEGORIES.map((category) => categoryChipMarkup(category, 0)).join('')}
          </nav>
          <div class="control-strip" aria-label="列表筛选与排序">
            <label class="search-control" for="fund-search">
              <input id="fund-search" type="search" data-search-input placeholder="输入代码或名称" autocomplete="off" />
              <button class="search-button" type="button" data-search-submit>搜索</button>
            </label>
            <label class="select-control" for="fund-sort">
              <span>排序</span>
              <select id="fund-sort" data-sort-select>
                <option value="">默认排序</option>
                <option value="premiumRate:desc">溢价率从高到低</option>
                <option value="premiumRate:asc">溢价率从低到高</option>
                <option value="quoteChangePercent:desc">场内涨跌幅从高到低</option>
                <option value="quoteChangePercent:asc">场内涨跌幅从低到高</option>
                <option value="navChangePercent:desc">净值涨跌幅从高到低</option>
                <option value="navChangePercent:asc">净值涨跌幅从低到高</option>
                <option value="turnoverAmount:desc">成交额从高到低</option>
                <option value="turnoverAmount:asc">成交额从低到高</option>
              </select>
            </label>
             <button class="watchlist-filter" type="button" data-purchase-limit-only aria-pressed="false">仅看限额</button>
             <button class="watchlist-filter" type="button" data-watchlist-only aria-pressed="false">仅看自选</button>
            <button class="watchlist-filter" type="button" data-filter-clear>清除</button>
             <button class="refresh-button watchlist-filter" type="button" data-snapshot-refresh>刷新</button>
           </div>
        </div>
      </section>

      <section class="data-panel" aria-labelledby="table-title">
        <div class="data-panel__header">
          <div class="section-heading">
            <p class="eyebrow">Snapshot</p>
            <h2 id="table-title">折溢价列表</h2>
          </div>
          <div class="status-stack" aria-label="数据源状态">
            <span class="status-badge status-badge--loading" data-source-status>加载中</span>
            <span class="status-badge status-badge--loading" data-stale-status>等待刷新</span>
          </div>
        </div>
        <div class="table-scroll" role="region" aria-label="基金折溢价表格" tabindex="0"></div>
      </section>

      <footer class="disclaimer" data-compliance-disclaimer>
        数据来源于公开信息；交易时段折溢价可基于盘中估值与最新场内价格计算，非交易时段基于官方已披露净值；不构成投资建议；投资有风险。
      </footer>
    </main>
  `;

  const shell = wrapper.firstElementChild as HTMLElement;
  shell.querySelector('[aria-label="基金折溢价表格"]')?.append(createFundTable());
  return shell;
};

export const mountDashboardShell = (root: HTMLElement, state: DashboardState): void => {
  if (
    !root.matches('[data-dashboard-shell]') ||
    !root.querySelector('[data-category-all]') ||
    !root.querySelector('[data-stale-status]') ||
    !root.querySelector('[data-search-input]') ||
    !root.querySelector('[data-search-submit]') ||
    !root.querySelector('[data-sort-select]') ||
     !root.querySelector('[data-snapshot-refresh]') ||
     !root.querySelector('[data-watchlist-only]') ||
    !root.querySelector('[data-purchase-limit-only]') ||
    !root.querySelector('[data-filter-clear]')
   ) {
    root.replaceWith(createDashboardShell());
  }

  renderDashboard(state);
};

const createFundTable = (): HTMLTableElement => {
  const table = document.createElement('table');
  table.className = 'fund-table';
  table.setAttribute('data-fund-table', '');

  const caption = document.createElement('caption');
  caption.textContent = '基金折溢价监控表';

  const head = document.createElement('thead');
  const headerRow = document.createElement('tr');
  for (const header of tableHeaders) {
    const cell = document.createElement('th');
    cell.scope = 'col';
    if (header.numeric) {
      cell.className = 'numeric';
    }
    if (header.sortKey) {
      cell.append(createSortButton(header));
    } else {
      cell.textContent = header.label;
    }
    headerRow.append(cell);
  }
  head.append(headerRow);

  const body = document.createElement('tbody');
  body.setAttribute('data-fund-rows', '');
  body.append(createEmptyRow());

  const colgroup = document.createElement('colgroup');
  for (let i = 0; i < tableHeaders.length; i++) {
    colgroup.append(document.createElement('col'));
  }

  table.append(colgroup, caption, head, body);
  return table;
};

const createSortButton = (header: HeaderConfig): HTMLButtonElement => {
  const button = document.createElement('button');
  button.className = 'sort-header-button';
  button.type = 'button';
  button.setAttribute('data-sort-button', '');
  button.dataset.sortKey = header.sortKey;
  button.dataset.sortDirection = 'none';
  button.setAttribute('aria-sort', 'none');

  const label = document.createElement('span');
  label.textContent = header.label;
  button.append(label, sortArrowElement());
  return button;
};

const sortArrowElement = (): SVGSVGElement => {
  const wrapper = document.createElement('span');
  wrapper.innerHTML = sortArrowMarkup();
  return wrapper.firstElementChild as SVGSVGElement;
};

const sortArrowMarkup = (): string =>
  '<svg class="sort-arrow" viewBox="0 0 12 16" aria-hidden="true"><path class="sort-arrow__up" d="M6 1 2 6h8L6 1Z"/><path class="sort-arrow__down" d="M6 15 2 10h8l-4 5Z"/></svg>';

export const renderDashboard = (state: DashboardState): void => {
	 renderCategoryChips(state);
  renderFilterControls(state);
  renderSortHeaders(state);
	 renderStatus(state);
	 renderRows(visibleFunds(state));
};

const categoryChipMarkup = (category: FundCategory, count: number): string =>
	 `<button class="category-chip" type="button" data-category="${category}" aria-pressed="false">${category}<span class="category-chip__count">${formatCategoryCount(count)}</span></button>`;

const allCategoryChipMarkup = (): string =>
	 `<button class="category-chip" type="button" data-category-all aria-pressed="true">全部<span class="category-chip__count">${formatCategoryCount(0)}</span></button>`;

const formatCategoryCount = (count: number): string => `(${count})`;

const projectMarkMarkup = (): string =>
  `<svg data-project-icon="" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M3 13.125C3 12.504 3.504 12 4.125 12h2.25c.621 0 1.125.504 1.125 1.125v6.75C7.5 20.496 6.996 21 6.375 21h-2.25A1.125 1.125 0 0 1 3 19.875v-6.75ZM9.75 8.625c0-.621.504-1.125 1.125-1.125h2.25c.621 0 1.125.504 1.125 1.125v11.25c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 0 1-1.125-1.125V8.625ZM16.5 4.125c0-.621.504-1.125 1.125-1.125h2.25C20.496 3 21 3.504 21 4.125v15.75c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 0 1-1.125-1.125V4.125Z"/></svg>`;

const renderCategoryChips = (state: DashboardState): void => {
	const counts = categoryCounts(state.items);
	 document.querySelectorAll<HTMLButtonElement>('[data-category]').forEach((chip) => {
		 const category = chip.dataset.category as FundCategory;
		 chip.setAttribute('aria-pressed', String(category === state.selectedCategory));
		 const count = chip.querySelector<HTMLElement>('.category-chip__count');
		 if (count) {
			 count.textContent = formatCategoryCount(counts.get(category) ?? 0);
		 }
	 });
	 const allCategory = document.querySelector<HTMLButtonElement>('[data-category-all]');
	 allCategory?.setAttribute('aria-pressed', String(state.selectedCategory === null));
	 const allCount = allCategory?.querySelector<HTMLElement>('.category-chip__count');
	 if (allCount) {
		 allCount.textContent = formatCategoryCount(state.items.length);
	 }
};

const categoryCounts = (items: FundSnapshot[]): Map<FundCategory, number> => {
	const counts = new Map<FundCategory, number>();
	for (const item of items) {
		counts.set(item.category, (counts.get(item.category) ?? 0) + 1);
	}
	return counts;
};

const renderFilterControls = (state: DashboardState): void => {
  const searchInput = document.querySelector<HTMLInputElement>('[data-search-input]');
  const sortSelect = document.querySelector<HTMLSelectElement>('[data-sort-select]');
  const watchlistOnly = document.querySelector<HTMLButtonElement>('[data-watchlist-only]');
  const purchaseLimitOnly = document.querySelector<HTMLButtonElement>('[data-purchase-limit-only]');

  if (searchInput && searchInput.value !== state.searchQuery) {
    searchInput.value = state.searchQuery;
  }
  if (sortSelect) {
    sortSelect.value = state.sortKey === null ? '' : `${state.sortKey}:${state.sortDirection}`;
  }
  if (watchlistOnly) {
    watchlistOnly.setAttribute('aria-pressed', String(state.watchlistOnly));
  }
  if (purchaseLimitOnly) {
    purchaseLimitOnly.setAttribute('aria-pressed', String(state.purchaseLimitOnly));
  }
};

const renderSortHeaders = (state: DashboardState): void => {
  document.querySelectorAll<HTMLButtonElement>('[data-sort-button]').forEach((button) => {
    const key = button.dataset.sortKey as SortKey;
    const active = state.sortKey === key;
    button.setAttribute('aria-sort', active ? ariaSort(state.sortDirection) : 'none');
    button.dataset.sortDirection = active ? state.sortDirection : 'none';
  });
};

const ariaSort = (direction: SortDirection): 'ascending' | 'descending' => (direction === 'asc' ? 'ascending' : 'descending');

const renderStatus = (state: DashboardState): void => {
  const sourceStatus = document.querySelector<HTMLElement>('[data-source-status]');
  const staleStatus = document.querySelector<HTMLElement>('[data-stale-status]');

  if (sourceStatus) {
    sourceStatus.className = `status-badge ${dashboardSourceClass(state.source)}`;
    sourceStatus.textContent = sourceStatusLabel(state.source, state.progress, state.stale);
  }
  if (staleStatus) {
    staleStatus.className = `status-badge ${staleClass(state.stale)}`;
    staleStatus.textContent = staleLabel(state.stale);
  }
};

const sourceStatusLabel = (source: DashboardState['source'], progress: ProgressState | null, stale: DashboardState['stale']): string => {
  const labels: Record<DashboardState['source'], string> = {
    loading: '数据源加载中',
    partial: '数据源部分可用',
    ready: '数据源正常',
    error: '数据源异常',
  };

  if (progress && progress.percent < 100) {
    return progressStatusLabel(progress);
  }
  if (stale === 'stale') {
    return '数据可能滞后';
  }
  return labels[source];
};

const progressStatusLabel = ({ label, percent }: ProgressState): string => `拉取数据 ${percent}% · ${label}`;

const dashboardSourceClass = (source: DashboardState['source']): string =>
  source === 'loading' ? 'status-badge--loading' : sourceClass(source);

const renderRows = (items: FundSnapshot[]): void => {
  const body = document.querySelector<HTMLElement>('[data-fund-rows]');
  if (!body) {
    return;
  }

  if (items.length === 0) {
    body.replaceChildren(createEmptyRow());
    return;
  }

  // Remove empty-row if present
  body.querySelector('[data-empty-row]')?.remove();

  // Build index of existing rows
  const existingRows = new Map<string, HTMLTableRowElement>();
  for (const row of Array.from(body.querySelectorAll<HTMLTableRowElement>('tr[data-fund-code]'))) {
    existingRows.set(row.dataset.fundCode!, row);
  }

  // Track desired order
  const desiredCodes = new Set(items.map((item) => item.code));

  // Remove rows no longer present
  for (const [code, row] of existingRows) {
    if (!desiredCodes.has(code)) {
      row.remove();
      existingRows.delete(code);
    }
  }

  // Update or insert rows in order
  let previousNode: Element | null = null;
  for (const item of items) {
    const existing = existingRows.get(item.code);
    if (existing) {
      updateFundRow(existing, item);
      // Ensure correct order
      if (previousNode !== null && existing.previousElementSibling !== previousNode) {
        previousNode.after(existing);
      }
      previousNode = existing;
    } else {
      const newRow = createFundRow(item);
      if (previousNode !== null) {
        previousNode.after(newRow);
      } else {
        body.prepend(newRow);
      }
      previousNode = newRow;
    }
  }
};

const updateFundRow = (row: HTMLTableRowElement, item: FundSnapshot): void => {
  const cells = row.cells;
  if (cells.length < 7) {
    return;
  }

  // Cell 0: fund heading (th)
  const nameEl = cells[0].querySelector('.fund-name');
  const codeEl = cells[0].querySelector('.fund-code');
  if (nameEl && nameEl.textContent !== item.name) {
    nameEl.textContent = item.name;
  }
  if (codeEl && codeEl.textContent !== item.code) {
    codeEl.textContent = item.code;
  }

  // Cell 1: tags
  updateTagsCell(cells[1], item);

  // Cell 2: NAV
  const navClass = `numeric ${numericClass(item.navChangePercent)}`;
  if (cells[2].className !== navClass) {
    cells[2].className = navClass;
  }
  updateValueStack(cells[2], formatValueWithPercent(item.nav, item.navChangePercent), formatNAVBasis(item));

  // Cell 3: quote price
  const quoteClass = `numeric ${numericClass(item.quoteChangePercent)}`;
  if (cells[3].className !== quoteClass) {
    cells[3].className = quoteClass;
  }
  updateValueStack(cells[3], formatValueWithPercent(item.quotePrice, item.quoteChangePercent), formatFundScale(item));

  // Cell 4: premium rate
  const premiumText = formatPercent(item.premiumRate);
  const premiumClass = `numeric ${numericClass(item.premiumRate)}`;
  if (cells[4].className !== premiumClass) {
    cells[4].className = premiumClass;
  }
  if (cells[4].textContent !== premiumText) {
    cells[4].textContent = premiumText;
  }

  // Cell 5: turnover
  updateValueStack(cells[5], formatPercent(item.turnoverRate), formatTurnoverAmount(item.turnoverAmount, item.turnoverAmountUnit));

  // Cell 6: watchlist button
  const watchButton = cells[6].querySelector<HTMLButtonElement>('[data-watchlist-action]');
  if (watchButton) {
    if (watchButton.dataset.inWatchlist !== String(item.inWatchlist)) {
      watchButton.dataset.inWatchlist = String(item.inWatchlist);
      watchButton.setAttribute('aria-label', watchlistLabel(item.inWatchlist));
    }
    watchButton.disabled = false;
  }
};

const updateTagsCell = (cell: HTMLTableCellElement, item: FundSnapshot): void => {
  const tags = cell.querySelectorAll('.fund-tag');
  if (tags.length < 2) {
    return;
  }
  const primary = tags[0];
  let expectedPrimary: string;
  let primaryLimitClass = false;
  let primarySuspendedClass = false;
  if (item.purchaseLimit) {
    expectedPrimary = `限额：${item.purchaseLimit}`;
    primaryLimitClass = true;
  } else {
    expectedPrimary = `申购：${item.purchaseStatus || '--'}`;
    primarySuspendedClass = !!item.purchaseStatus?.includes('暂停');
  }
  if (primary.textContent !== expectedPrimary) {
    primary.textContent = expectedPrimary;
  }
  primary.classList.toggle('fund-tag--limit', primaryLimitClass);
  primary.classList.toggle('fund-tag--suspended', primarySuspendedClass);

  const redemption = tags[1];
  const expectedRedemption = `赎回：${item.redemptionStatus || '--'}`;
  if (redemption.textContent !== expectedRedemption) {
    redemption.textContent = expectedRedemption;
  }
  redemption.classList.toggle('fund-tag--suspended', !!item.redemptionStatus?.includes('暂停'));
};

const updateValueStack = (cell: HTMLTableCellElement, primary: string, secondary: string): void => {
  const stack = cell.querySelector('.value-stack');
  if (!stack) {
    return;
  }
  const mainEl = stack.querySelector('.value-stack__primary');
  const detailEl = stack.querySelector('.value-stack__secondary');
  if (mainEl && mainEl.textContent !== primary) {
    mainEl.textContent = primary;
  }
  if (detailEl && detailEl.textContent !== secondary) {
    detailEl.textContent = secondary;
  }
};

const createFundRow = (item: FundSnapshot): HTMLTableRowElement => {
  const row = document.createElement('tr');
  row.dataset.fundCode = item.code;

  row.append(
    createFundHeading(item),
    createTagsCell(item),
		 createValueStackCell(formatValueWithPercent(item.nav, item.navChangePercent), formatNAVBasis(item), `numeric ${numericClass(item.navChangePercent)}`),
		 createValueStackCell(formatValueWithPercent(item.quotePrice, item.quoteChangePercent), formatFundScale(item), `numeric ${numericClass(item.quoteChangePercent)}`),
		 createTextCell(formatPercent(item.premiumRate), `numeric ${numericClass(item.premiumRate)}`),
    createValueStackCell(formatPercent(item.turnoverRate), formatTurnoverAmount(item.turnoverAmount, item.turnoverAmountUnit), 'numeric'),
    createWatchlistCell(item),
  );

  return row;
};

const createFundHeading = (item: FundSnapshot): HTMLTableCellElement => {
  const heading = document.createElement('th');
  heading.scope = 'row';

  const name = document.createElement('span');
  name.className = 'fund-name';
  name.textContent = item.name;

  const code = document.createElement('span');
  code.className = 'fund-code';
  code.textContent = item.code;

  heading.append(name, code);
  return heading;
};

const createTextCell = (text: string, className?: string): HTMLTableCellElement => {
  const cell = document.createElement('td');
  if (className) {
    cell.className = className;
  }
  cell.textContent = text;
  return cell;
};

const createTagsCell = (item: FundSnapshot): HTMLTableCellElement => {
  const cell = document.createElement('td');
  const tags = document.createElement('span');
  tags.className = 'fund-tags';

  const primary = document.createElement('span');
  primary.className = 'fund-tag';
  if (item.purchaseLimit) {
    primary.classList.add('fund-tag--limit');
    primary.textContent = `限额：${item.purchaseLimit}`;
  } else {
    primary.textContent = `申购：${item.purchaseStatus || '--'}`;
    if (item.purchaseStatus?.includes('暂停')) {
      primary.classList.add('fund-tag--suspended');
    }
  }

  const redemption = document.createElement('span');
  redemption.className = 'fund-tag';
  redemption.textContent = `赎回：${item.redemptionStatus || '--'}`;
  if (item.redemptionStatus?.includes('暂停')) {
    redemption.classList.add('fund-tag--suspended');
  }

  tags.append(primary, redemption);
  cell.append(tags);
  return cell;
};

const createValueStackCell = (primary: string, secondary: string, className?: string): HTMLTableCellElement => {
  const cell = document.createElement('td');
  if (className) {
    cell.className = className;
  }

  const stack = document.createElement('span');
  stack.className = 'value-stack';

  const main = document.createElement('span');
  main.className = 'value-stack__primary';
  main.textContent = primary;

  const detail = document.createElement('span');
  detail.className = 'value-stack__secondary';
  detail.textContent = secondary;

  stack.append(main, detail);
  cell.append(stack);
  return cell;
};

const createWatchlistCell = (item: FundSnapshot): HTMLTableCellElement => {
  const cell = document.createElement('td');
  const button = document.createElement('button');
  const label = watchlistLabel(item.inWatchlist);

  button.className = 'watchlist-button';
  button.type = 'button';
  button.setAttribute('data-watchlist-action', '');
  button.dataset.code = item.code;
  button.dataset.inWatchlist = String(item.inWatchlist);
  button.setAttribute('aria-label', label);
  button.append(createHeartIcon());

  cell.append(button);
  return cell;
};

const createHeartIcon = (): SVGSVGElement => {
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('fill', 'none');
  svg.setAttribute('viewBox', '0 0 24 24');
  svg.setAttribute('stroke-width', '1.5');
  svg.setAttribute('stroke', 'currentColor');
  svg.setAttribute('aria-hidden', 'true');
  svg.setAttribute('data-watchlist-icon', '');

  const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
  path.setAttribute('stroke-linecap', 'round');
  path.setAttribute('stroke-linejoin', 'round');
  path.setAttribute('d', 'M21 8.25c0-2.485-2.099-4.5-4.688-4.5-1.935 0-3.597 1.126-4.312 2.733-.715-1.607-2.377-2.733-4.313-2.733C5.1 3.75 3 5.765 3 8.25c0 7.22 9 12 9 12s9-4.78 9-12Z');

  svg.append(path);
  return svg;
};

const createEmptyRow = (): HTMLTableRowElement => {
  const row = document.createElement('tr');
  row.className = 'empty-row';
  row.setAttribute('data-empty-row', '');

  const cell = document.createElement('td');
  cell.colSpan = tableHeaders.length;

  const state = document.createElement('div');
  state.className = 'empty-state';

  const orb = document.createElement('span');
  orb.className = 'empty-state__orb';
  orb.setAttribute('aria-hidden', 'true');

  const title = document.createElement('strong');
  title.textContent = '暂无匹配基金';

  const copy = document.createElement('span');
  copy.textContent = '请等待数据拉取完成，或调整分类、搜索、自选、限额筛选。';

  state.append(orb, title, copy);
  cell.append(state);
  row.append(cell);
  return row;
};

const sourceClass = (source: SourceState): string => {
  const classes: Record<SourceState, string> = {
    ready: 'status-badge--ready',
    partial: 'status-badge--loading',
    error: 'status-badge--error',
  };

  return classes[source];
};

const staleClass = (stale: DashboardState['stale']): string => {
  const classes: Record<DashboardState['stale'], string> = {
    fresh: 'status-badge--ready',
    stale: 'status-badge--loading',
    closed: 'status-badge--loading',
    unknown: 'status-badge--loading',
  };

  return classes[stale];
};

const staleLabel = (stale: DashboardState['stale']): string => {
	const labels: Record<DashboardState['stale'], string> = {
		fresh: '交易时段',
		stale: '交易时段',
		closed: '非交易时段',
    unknown: '等待刷新',
  };

  return labels[stale];
};

const watchlistLabel = (inWatchlist: boolean): string => (inWatchlist ? '取消自选' : '加入自选');

const formatNumber = (value: number | null): string => (value === null ? '--' : value.toFixed(4));

const formatPercent = (value: number | null): string => (value === null ? '--' : `${value.toFixed(2)}%`);

const numericClass = (value: number | null): string => {
	if (value === null || value === 0) {
		return '';
	}
	return value > 0 ? 'numeric--positive' : 'numeric--negative';
};

const formatValueWithPercent = (value: number | null, percent: number | null): string => {
  const formatted = formatNumber(value);
  return percent === null ? formatted : `${formatted} (${formatPercent(percent)})`;
};

const formatTurnoverAmount = (value: number | null, unit: string): string => (value === null ? '--' : `${value.toLocaleString('zh-CN')} ${unit}`);

const formatFundScale = (item: FundSnapshot): string => {
  if (item.fundScale) {
    return `规模 ${item.fundScale}`;
  }
  return '规模 --';
};

const formatNAVBasis = (item: FundSnapshot): string => {
  const date = item.navDate || '--';
  const basis = item.navBasis.includes('估值') ? '估值' : '净值';
  return `${basis} · ${date}`;
};
