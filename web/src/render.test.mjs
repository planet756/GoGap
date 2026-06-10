import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';

const renderSource = readFileSync(new URL('./render.ts', import.meta.url), 'utf8');
const cssSource = readFileSync(new URL('./main.css', import.meta.url), 'utf8');
const filtersSource = readFileSync(new URL('./filters.ts', import.meta.url), 'utf8');
const stateSource = readFileSync(new URL('./state.ts', import.meta.url), 'utf8');
const mainSource = readFileSync(new URL('./main.ts', import.meta.url), 'utf8');
const apiSource = readFileSync(new URL('./api.ts', import.meta.url), 'utf8');
const templateSource = readFileSync(new URL('../../internal/api/templates/fund_table.html', import.meta.url), 'utf8');
const dashboardShellSource = readFileSync(new URL('../../internal/api/templates/dashboard_shell.html', import.meta.url), 'utf8');
const staticShellSource = readFileSync(new URL('../index.html', import.meta.url), 'utf8');

test('rendering shows category counts on chips', () => {
  assert.match(renderSource, /category-chip__count/, 'category chips must render fund counts');
  assert.match(renderSource, /categoryCounts/, 'rendering must derive category counts from state items');
  assert.match(renderSource, /formatCategoryCount/, 'category counts must be formatted before display');
  assert.match(renderSource, /`\(\$\{count\}\)`/, 'category counts must be wrapped in parentheses');
  assert.match(cssSource, /\.category-chip__count\s*{[^}]*font-size:\s*0\.72rem/s, 'category counts must use a smaller font');
});

test('rendering does not show error details in the page', () => {
  assert.doesNotMatch(renderSource, /errors\.join/, 'UI must not concatenate backend error details into visible status text');
  assert.doesNotMatch(renderSource, /sourceStatusLabel\([^)]*errors/, 'source status formatter must not receive raw error details');
});

test('rendering applies red-positive green-negative classes to changes and premium', () => {
  assert.match(renderSource, /numericClass\(item\.navChangePercent\)/, 'NAV change percent must receive a sign class');
  assert.match(renderSource, /numericClass\(item\.quoteChangePercent\)/, 'quote change percent must receive a sign class');
  assert.match(renderSource, /numericClass\(item\.premiumRate\)/, 'premium rate must receive a sign class');
  assert.match(cssSource, /\.numeric--positive[^}]*var\(--color-danger\)/s, 'positive values must use the red danger color');
  assert.match(cssSource, /\.numeric--negative[^}]*var\(--color-success\)/s, 'negative values must use the green success color');
  assert.match(cssSource, /\.numeric--positive[^}]*--numeric-color:\s*var\(--color-danger\)/s, 'positive stacked values must set the inherited numeric color');
  assert.match(cssSource, /\.numeric--negative[^}]*--numeric-color:\s*var\(--color-success\)/s, 'negative stacked values must set the inherited numeric color');
  assert.match(cssSource, /\.value-stack__primary[^}]*var\(--numeric-color,\s*var\(--color-text\)\)/s, 'stacked primary values must inherit the numeric color when present');
});

test('rendering shows snapshot progress percentage in source status', () => {
  assert.match(renderSource, /ProgressState/, 'rendering must type progress state');
  assert.match(renderSource, /progressStatusLabel/, 'rendering must format progress status text');
  assert.match(renderSource, /拉取数据.*percent/, 'progress label must include pull progress percent');
});

test('rendering aligns headers and exposes limit filter', () => {
  assert.match(renderSource, /申购\/赎回/, 'header must match purchase and redemption tags');
  assert.match(templateSource, /申购\/赎回/, 'server template header must match client header');
  assert.doesNotMatch(renderSource, /数据连接将在后续任务接入/, 'empty copy must not mention future integration');
  assert.match(renderSource, /data-purchase-limit-only/, 'dashboard must render only-limit filter button');
  assert.match(stateSource, /purchaseLimitOnly/, 'state must track only-limit filter');
  assert.match(filtersSource, /purchaseLimit\.trim\(\) !== ''/, 'filter must match actual purchase limits only');
});

test('rendering exposes sortable numeric headers with svg arrows', () => {
  assert.match(renderSource, /data-sort-button/, 'sortable headers must be buttons');
  assert.match(renderSource, /sortArrowMarkup/, 'sortable headers must include SVG arrow markup');
  assert.match(renderSource, /navChangePercent/, 'NAV header sort must use NAV change percent');
  assert.match(renderSource, /quoteChangePercent/, 'quote header sort must use quote change percent');
  assert.match(templateSource, /data-sort-key="navChangePercent"/, 'server table must expose NAV change percent sorting');
  assert.match(templateSource, /data-sort-key="quoteChangePercent"/, 'server table must expose quote change percent sorting');
  assert.match(templateSource, /data-sort-key="premiumRate"/, 'server table must expose premium sorting');
  assert.match(templateSource, /sort-arrow/, 'server table must include visible sort arrows before hydration');
  assert.match(staticShellSource, /data-sort-key="navChangePercent"/, 'static table must expose NAV change percent sorting');
  assert.match(staticShellSource, /data-sort-key="quoteChangePercent"/, 'static table must expose quote change percent sorting');
  assert.match(staticShellSource, /data-sort-key="premiumRate"/, 'static table must expose premium sorting');
  assert.match(staticShellSource, /sort-arrow/, 'static table must include visible sort arrows before hydration');
  assert.match(mainSource, /\[data-sort-button\]/, 'main must bind sortable header clicks');
  assert.match(cssSource, /sort-arrow/, 'sort arrows must have CSS');
});

test('watchlist action updates optimistically before request settles', () => {
  assert.match(mainSource, /const previousWatchlistState/, 'watchlist handler must remember previous state for rollback');
  assert.match(mainSource, /updateFundWatchlist\(code, !shouldRemove\)[\s\S]*await/, 'watchlist state must update before awaiting request');
  assert.match(mainSource, /updateFundWatchlist\(code, previousWatchlistState\)/, 'watchlist failure must roll back previous state');
});

test('watchlist requests carry anonymous visitor id', () => {
  assert.match(apiSource, /visitorStorageKey = 'gogap\.visitorId'/, 'frontend must name a stable anonymous visitor id key');
  assert.match(apiSource, /localStorage[\s\S]*visitorStorageKey/, 'frontend must persist an anonymous local visitor id');
  assert.match(apiSource, /X-GoGap-Visitor-Id/, 'watchlist and snapshot API calls must send visitor id header');
});

test('refresh button forces a backend snapshot refresh', () => {
  assert.match(apiSource, /export const refreshSnapshot/, 'frontend API must expose a forced snapshot refresh helper');
  assert.match(apiSource, /fetchSnapshot\(snapshotEndpoint,[\s\S]*method:\s*'POST'/, 'snapshot refresh must POST to the snapshot endpoint');
  assert.match(mainSource, /refreshDashboardSnapshot\(fetchSnapshot, refreshButton, true\)/, 'refresh button must force backend refresh');
  assert.match(mainSource, /forceRefresh \? await refreshSnapshot\(fetchSnapshot\) : await loadInitialSnapshot\(fetchSnapshot\)/, 'initial load must remain GET while manual refresh uses POST');
});

test('rendering keeps requested NAV and freshness copy', () => {
  assert.match(renderSource, /formatValueWithPercent\(item\.nav, item\.navChangePercent\)/, 'NAV primary must keep appending change percent when present');
  assert.match(renderSource, /formatValueWithPercent\(item\.quotePrice, item\.quoteChangePercent\)/, 'quote primary must keep appending change percent when present');
  assert.match(renderSource, /formatValueWithPercent[\s\S]*`\$\{formatted\} \(\$\{formatPercent\(percent\)\}\)`/, 'value formatter must render change percent in parentheses');
  assert.match(renderSource, /item\.navBasis\.includes\('估值'\) \? '估值' : '净值'/, 'NAV basis label must be exactly NAV or estimate');
  assert.match(renderSource, /sourceStatusLabel\(state\.source, state\.progress, state\.stale\)/, 'source status must know when data may be stale');
  assert.match(renderSource, /stale === 'stale'[\s\S]*'数据可能滞后'/, 'stale data copy must belong to the source status label');
  assert.match(renderSource, /fresh:\s*'交易时段'/, 'fresh status must be described as trading session');
  assert.match(renderSource, /stale:\s*'交易时段'/, 'stale status must still be described as trading session');
  assert.match(renderSource, /closed:\s*'非交易时段'/, 'closed status must be described as non-trading session');
  assert.doesNotMatch(renderSource, /stale:\s*'数据可能滞后'/, 'time-session copy must not describe data freshness or lag');
  assert.doesNotMatch(renderSource, /数据新鲜/, 'client shell must not show vague freshness copy');
  assert.doesNotMatch(renderSource, /新鲜度未知/, 'client shell must not show unknown freshness copy');
});

test('table and filter layout matches requested fixed visual behavior', () => {
  assert.match(cssSource, /\.filter-panel\s*{[^}]*position:\s*sticky/s, 'filter panel must stay fixed while scrolling');
  assert.match(cssSource, /\.fund-table th,\s*\.fund-table td\s*{[^}]*text-align:\s*center/s, 'table cells and headers must be centered');
  assert.match(cssSource, /\.fund-table thead th\s*{[^}]*position:\s*sticky/s, 'table header cells must stay sticky');
  assert.doesNotMatch(cssSource, /\.watchlist-filter\s*{[^}]*width:\s*100%/s, 'watchlist filter must not be forced full width');
  assert.match(cssSource, /\.control-strip\s*{[^}]*grid-template-columns:\s*minmax\(10rem,\s*16rem\)/s, 'search control must be narrower on desktop');
  assert.doesNotMatch(cssSource, /\.search-control:focus-within/, 'search control must not have a focus-within style override');
  assert.match(cssSource, /--filter-sort-label-width:\s*2rem/, 'sort label must stay compact');
  assert.match(cssSource, /--filter-sort-width:\s*9rem/, 'sort select only needs enough room for its text');
  assert.match(cssSource, /\.select-control select\s*{[^}]*padding:\s*0 var\(--space-3\) 0 0/s, 'sort select must keep room for its text');
  assert.doesNotMatch(cssSource, /\.search-control input:focus-visible/, 'search input must not render its own focus styling');
  assert.match(cssSource, /\.watchlist-filter\s*{[^}]*width:\s*fit-content/s, 'watchlist filter buttons must stay compact');
  assert.doesNotMatch(cssSource, /\.hero-panel\s*{[^}]*position:\s*sticky/s, 'hero header must not be sticky while scrolling');
});

test('limit and watchlist filters stay adjacent in requested order', () => {
  assert.match(renderSource, /data-purchase-limit-only[\s\S]*data-watchlist-only[\s\S]*data-filter-clear[\s\S]*data-snapshot-refresh/, 'limit and watchlist filters must stay adjacent before clear and refresh');
  assert.match(dashboardShellSource, /data-purchase-limit-only[\s\S]*data-watchlist-only[\s\S]*data-filter-clear[\s\S]*data-snapshot-refresh/, 'server shell must keep limit/watchlist/clear/refresh order');
  assert.match(staticShellSource, /data-purchase-limit-only[\s\S]*data-watchlist-only[\s\S]*data-filter-clear[\s\S]*data-snapshot-refresh/, 'static shell must keep limit/watchlist/clear/refresh order');
  assert.match(mainSource, /\[data-filter-clear\]/, 'main must bind the clear filter button');
  assert.match(stateSource, /clearDashboardFilters/, 'state must expose a filter clearing helper');
});

test('dashboard filter state persists across reloads', () => {
  assert.match(stateSource, /dashboardStateStorageKey/, 'dashboard filters must use a stable storage key');
  assert.match(stateSource, /hydrateDashboardPreferences/, 'state must hydrate persisted filter preferences');
  assert.match(stateSource, /persistDashboardPreferences/, 'state changes must persist filter preferences');
  assert.match(mainSource, /hydrateDashboardPreferences\(\)/, 'dashboard must hydrate filters before mounting');
});

test('stream snapshots preserve existing watchlist markers', () => {
  assert.match(stateSource, /applyStreamSnapshot/, 'state must expose a separate stream snapshot path');
  assert.match(stateSource, /watchlistByCode/, 'stream snapshots must index current watchlist markers by code');
  assert.match(mainSource, /applyStreamSnapshot\(snapshot\)/, 'SSE updates must use watchlist-preserving snapshot application');
});

test('categories include Hong Kong in requested order', () => {
  assert.match(renderSource, /FUND_CATEGORIES\.map/, 'rendering must use shared category order');
  const typesSource = readFileSync(new URL('./types.ts', import.meta.url), 'utf8');
  assert.match(typesSource, /香港/, 'frontend must know Hong Kong category');
  assert.match(typesSource, /\['QDII', '商品', '香港', '主动LOF', '指数LOF', 'ETF', '债券货币'\]/, 'category order must put 商品 and 香港 immediately after QDII and exclude empty 其他LOF and REITs');
  assert.doesNotMatch(typesSource, /其他LOF/, 'frontend category list must not include 其他LOF');
  assert.doesNotMatch(typesSource, /REITs/, 'frontend category list must not include REITs');
  assert.doesNotMatch(dashboardShellSource, /data-category="其他LOF"/, 'server shell must not render 其他LOF filter');
  assert.doesNotMatch(staticShellSource, /data-category="其他LOF"/, 'static shell must not render 其他LOF filter');
  assert.doesNotMatch(dashboardShellSource, /data-category="REITs"/, 'server shell must not render REITs filter');
  assert.doesNotMatch(staticShellSource, /data-category="REITs"/, 'static shell must not render REITs filter');
  assert.match(dashboardShellSource, /data-category="QDII"[\s\S]*data-category="商品"[\s\S]*data-category="香港"[\s\S]*data-category="主动LOF"/, 'server shell must keep requested category order');
  assert.match(staticShellSource, /data-category="QDII"[\s\S]*data-category="商品"[\s\S]*data-category="香港"[\s\S]*data-category="主动LOF"/, 'static shell must keep requested category order');
});

test('category filter layout is visually grouped and wraps cleanly', () => {
  assert.match(cssSource, /\.chip-group\s*{[^}]*padding:\s*var\(--space-2\)/s, 'category group must have panel padding');
  assert.match(cssSource, /\.chip-group\s*{[^}]*background:\s*linear-gradient/s, 'category group must have a refined surface treatment');
  assert.match(cssSource, /\.category-chip\s*{[^}]*box-shadow:\s*inset/s, 'category chips must have subtle chip depth');
  assert.doesNotMatch(cssSource, /@media\s*\(max-width:\s*760px\)[\s\S]*\.chip-group\s*{[^}]*flex-wrap:\s*nowrap/s, 'mobile category chips must keep wrapping instead of overflowing horizontally');
});
