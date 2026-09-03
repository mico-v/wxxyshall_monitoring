if ('serviceWorker' in navigator) {
  navigator.serviceWorker.register('/sw.js')
    .catch(function(e) { console.warn('SW 注册失败:', e); });
}
"use strict";

/* ============ 主题切换(浅/深) ============ */
const THEME_KEY = "elec-theme";
function applyTheme(t) {
  document.documentElement.setAttribute("data-theme", t);
  document.getElementById("theme-btn").textContent = t === "dark" ? "浅色" : "深色";
}
function initTheme() {
  const saved = localStorage.getItem(THEME_KEY);
  if (saved) applyTheme(saved);
}
document.getElementById("theme-btn").addEventListener("click", () => {
  const cur = document.documentElement.getAttribute("data-theme") ||
               (matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");
  const next = cur === "dark" ? "light" : "dark";
  applyTheme(next);
  localStorage.setItem(THEME_KEY, next);
  render();   // 重渲图表以适配新主题色
});

/* ============ SSE 实时推送 ============ */
let eventSource = null;
let sseConnected = false;
let pollTimer = null;
let sseRefreshTimer = null;
const ACTIVE_JOB_SESSION = "elec-active-collect-job";
let activeJobId = sessionStorage.getItem(ACTIVE_JOB_SESSION) || "";

function setActiveJobId(jobId) {
  activeJobId = jobId || "";
  if (activeJobId) sessionStorage.setItem(ACTIVE_JOB_SESSION, activeJobId);
  else sessionStorage.removeItem(ACTIVE_JOB_SESSION);
}

function scheduleReadingRefresh() {
  clearTimeout(sseRefreshTimer);
  sseRefreshTimer = setTimeout(() => {
    sseRefreshTimer = null;
    refresh();
  }, 300);
}

function connectSSE() {
  if (eventSource) { eventSource.close(); eventSource = null; }
  sseConnected = false;
  if (!state.room && !state.showHomepage) {
    startPollingFallback();
    return;
  }
  stopPollingFallback();
  const q = new URLSearchParams();
  if (state.room) {
    q.set("campus", state.room.campus);
    q.set("building", state.room.building);
    q.set("room", state.room.room);
  }
  const eventURL = "/api/events" + (q.toString() ? "?" + q.toString() : "");
  try {
    eventSource = new EventSource(eventURL);
  } catch (e) {
    console.warn('SSE 初始化失败:', e);
    startPollingFallback();
    return;
  }

  eventSource.addEventListener('reading', function(e) {
    try {
      const data = JSON.parse(e.data);
      if (!state.room ||
          (state.room.campus === data.campus &&
           state.room.building === data.building &&
           state.room.room === data.room)) {
        scheduleReadingRefresh();
      }
    } catch (err) { console.warn('SSE reading 解析失败:', err); }
  });

  eventSource.addEventListener('heartbeat', function() {
    sseConnected = true;
  });

  eventSource.onerror = function() {
    sseConnected = false;
    eventSource.close();
    startPollingFallback();
  };

  sseConnected = true;
}

function startPollingFallback() {
  if (pollTimer) { stopPollingFallback(); }
  pollTimer = setInterval(function() {
    if (!sseConnected) { refresh(); }
  }, 60000);
}

function stopPollingFallback() {
  if (pollTimer) {
    clearInterval(pollTimer);
    pollTimer = null;
  }
}
const fmt = v => (v === null || v === undefined || isNaN(v))
  ? "—"
  : Number(v).toLocaleString("zh-CN", { minimumFractionDigits: 2, maximumFractionDigits: 2 });

function fullTs(ts) {
  const d = new Date(ts.replace(" ", "T"));
  const p = n => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

/* 低电量分级: >=30 充足, 10-30 偏低, 5-10 不足, <5 欠费/临界 */
function balanceStatus(v) {
  if (v === null || v === undefined || isNaN(v)) return { level: "", text: "暂无数据" };
  if (v < 0)   return { level: "critical", text: "已欠费" };
  if (v < 5)   return { level: "critical", text: "即将断电" };
  if (v < 10)  return { level: "serious",  text: "余额不足" };
  if (v < 30)  return { level: "warning",  text: "余额偏低" };
  return { level: "good", text: "余额充足" };
}

/* ============ 多宿舍:配色与分组 ============ */
const PALETTE = ["#2a78d6", "#d97706", "#059669", "#be4d9b", "#dc2626",
                 "#7c6cd6", "#0e9db0", "#9a9e1f", "#d0660e", "#4563c9"];
const roomColor = i => PALETTE[i % PALETTE.length];
function roomColorFor(r) {
  const text = roomKey(r);
  let hash = 0;
  for (let i = 0; i < text.length; i++) hash = (hash * 31 + text.charCodeAt(i)) | 0;
  return PALETTE[Math.abs(hash) % PALETTE.length];
}
const esc = s => String(s ?? "").replace(/[&<>"']/g,
  c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const roomKey = r => [r.campus, r.building, r.room].join("|");
function targetLabel(t) {
  const label = String(t?.label ?? "").trim();
  return label || [t?.campus, t?.building, t?.room].filter(Boolean).join("/") || "未知宿舍";
}
function labelForRow(r) {
  const t = state.targets.find(x => roomKey(x) === roomKey(r));
  return t ? targetLabel(t) : (r.display_label || r.room_label || roomKey(r) || "未知宿舍");
}
function groupByRoom(rows) {
  const m = new Map();
  rows.forEach(r => {
    const k = roomKey(r);
    if (!m.has(k)) m.set(k, { label: labelForRow(r), rows: [] });
    m.get(k).rows.push(r);
  });
  const order = new Map(state.targets.map((target, index) => [roomKey(target), index]));
  const g = [...m.entries()].map(([key, group]) => ({ ...group, key }));
  g.sort((a, b) => {
    const ai = order.has(a.key) ? order.get(a.key) : Number.MAX_SAFE_INTEGER;
    const bi = order.has(b.key) ? order.get(b.key) : Number.MAX_SAFE_INTEGER;
    return ai - bi || a.label.localeCompare(b.label, "zh");
  });
  return g;
}

/* ============ 数据获取 ============ */
let state = {
  days: 0,
  data: [],
  targets: [],        // 来自 /api/config
  defaults: { feeitemid: 409, appId: 34 },
  room: null,         // 当前查看的宿舍 {campus,building,room,label},null=全部
  adminAuthRequired: false,
  showHomepage: true,
  homepageUnlocked: false,
  tablePage: 1,
  tablePageSize: 10,
};

/* ============ 路径导航(History API) ============ */
// 每间宿舍一个独立 URL:/room/<campus>/<building>/<room>;"/" = 全部宿舍全览。
// 视图状态(state.room)与 URL 同步:pushState 记录跳转,popstate 响应前进/后退。
const ROOM_PATH = /^\/room\/([^/]+)\/([^/]+)\/([^/]+)\/?$/;
function roomFromPath() {
  const m = location.pathname.match(ROOM_PATH);
  return m ? { campus: decodeURIComponent(m[1]),
               building: decodeURIComponent(m[2]),
               room: decodeURIComponent(m[3]) } : null;
}
function pathFor(room) {
  if (!room) return "/";
  return "/room/" + [room.campus, room.building, room.room]
           .map(encodeURIComponent).join("/");
}
function navigate(room, replace = false) {
  state.room = room;
  state.tablePage = 1;
  window.scrollTo(0, 0);
  history[replace ? "replaceState" : "pushState"]({ room }, "", pathFor(room));
  refresh();
  connectSSE();
}
window.addEventListener("popstate", async () => {   // 浏览器前进/后退
  state.room = roomFromPath();
  state.tablePage = 1;
  window.scrollTo(0, 0);
  if (!state.room && !state.showHomepage && !state.homepageUnlocked) {
    await unlockHomepage();
  }
  refresh();
  connectSSE();
});

function readingsURL() {
  const q = new URLSearchParams();
  if (state.days) q.set("days", state.days);
  if (state.room) {
    q.set("campus", state.room.campus);
    q.set("building", state.room.building);
    q.set("room", state.room.room);
  }
  const suffix = q.toString();
  return "/api/readings" + (suffix ? "?" + suffix : "");
}

async function fetchReadings() {
  const headers = new Headers();
  if (!state.room && !state.showHomepage) {
    if (!state.homepageUnlocked || !adminKey) throw new Error("主页需要管理密钥");
    headers.set("Authorization", `Bearer ${adminKey}`);
  }
  const r = await fetch(readingsURL(), { cache: "no-store", headers });
  if (!r.ok) throw new Error("HTTP " + r.status);
  return await r.json();
}

async function refreshPublicConfig() {
  const q = new URLSearchParams();
  if (state.room) {
    q.set("campus", state.room.campus);
    q.set("building", state.room.building);
    q.set("room", state.room.room);
  }
  const headers = new Headers();
  if (!state.room && state.homepageUnlocked && adminKey) {
    headers.set("Authorization", `Bearer ${adminKey}`);
  }
  const suffix = q.toString();
  const r = await fetch("/api/config" + (suffix ? "?" + suffix : ""), { cache: "no-store", headers });
  if (!r.ok) throw new Error("HTTP " + r.status);
  const cfg = await r.json();
  const nextTargets = cfg.targets || [];
  const changed = JSON.stringify(nextTargets) !== JSON.stringify(state.targets);
  const previousHomepageVisibility = state.showHomepage;
  state.targets = nextTargets;
  if (cfg.defaults) state.defaults = cfg.defaults;
  state.adminAuthRequired = cfg.admin_auth_required === true;
  state.showHomepage = cfg.show_homepage !== false;
  if (!state.room) {
    document.body.dataset.showHomepage = String(state.showHomepage);
    if (!state.showHomepage && !state.homepageUnlocked) {
      document.body.classList.remove("homepage-unlocked");
    }
  }
  if (changed && state.data.length) render();
  else if (previousHomepageVisibility !== state.showHomepage) renderBackButton();
  if (previousHomepageVisibility !== state.showHomepage && (eventSource || pollTimer)) connectSSE();
  return cfg;
}

let configPollTimer = null;
function startConfigPolling() {
  if (configPollTimer) clearInterval(configPollTimer);
  configPollTimer = setInterval(() => {
    refreshPublicConfig().then(async () => {
      if (!state.room && !state.showHomepage && !state.homepageUnlocked) {
        await unlockHomepage();
        await refreshPublicConfig();
        await refresh();
      }
    }).catch(e => console.warn("配置刷新失败:", e));
  }, 30000);
}

/* 刷新序号:并发刷新时丢弃过期结果,避免旧 fetch 覆盖新数据(钻取/返回快速切换时) */
let refreshSeq = 0;
async function refresh() {
  const seq = ++refreshSeq;
  const dash = document.getElementById("dash");
  dash.classList.add("refreshing");
  try {
    const data = await fetchReadings();
    if (seq !== refreshSeq) return;      // 已有更新的刷新,丢弃本次结果
    state.data = data;
    render();
  } catch (e) {
    if (seq !== refreshSeq) return;
    console.error("读取失败:", e);
  } finally {
    if (seq === refreshSeq) dash.classList.remove("refreshing");
  }
}

/* ============ 渲染 ============ */
function render() {
  state.groups = groupByRoom(state.data);
  state.colorByKey = {};
  state.groups.forEach(g => {
    state.colorByKey[roomKey(g.rows[0])] = roomColorFor(g.rows[0]);
  });
  renderBackButton();
  renderKpis();
  renderChart();
  const detailCard = document.getElementById("detail-card");
  if (detailCard) detailCard.hidden = !state.room;
  if (state.room) renderTable();

  // 每间宿舍一个独立页面标题
  const roomLabel = state.room && state.groups.length ? state.groups[0].label : "";
  document.title = roomLabel ? `宿舍电费 · ${roomLabel}` : "宿舍电费监控";
}

function renderBackButton() {
  const btn = document.getElementById("back-btn");
  if (btn) btn.hidden = !state.room || !state.showHomepage;
}

function renderKpis() {
  const holder = document.getElementById("kpis");
  if (!state.data.length) {
    holder.innerHTML = '<div class="empty">暂无读数，可点击“立即采集”获取第一条记录</div>';
    document.getElementById("room-sub").textContent = "—";
    return;
  }
  if (state.groups.length === 1) renderSingleKpis(holder, state.groups[0]);
  else renderRoomKpis(holder, state.groups);
}

function renderSingleKpis(holder, group) {
  holder.className = "kpis";
  const data = group.rows;
  const last = data[data.length - 1];
  const room = labelForRow(last);
  document.getElementById("room-sub").textContent = room;

  // 变化量(同一宿舍内相对上一次读数)
  let deltaText = "", deltaClass = "flat";
  if (data.length >= 2) {
    const prev = data[data.length - 2].surplus_charge;
    const cur = last.surplus_charge;
    if (cur !== null && prev !== null) {
      const d = cur - prev;
      deltaClass = Math.abs(d) < 1e-9 ? "flat" : (d > 0 ? "up" : "down");
      deltaText = (d > 0 ? "+" : "") + fmt(d) + " kWh";
    }
  }

  const st = balanceStatus(last.surplus_charge);
  const stHtml = st.level
    ? `<div class="status ${st.level}"><span class="dot"></span>${st.text}</div>`
    : "";

  holder.innerHTML = `
    <div class="kpi">
      <div class="label">当前剩余电量</div>
      <div class="value hero">${fmt(last.surplus_charge)}<span class="unit">&nbsp;kWh</span></div>
      <div class="delta ${deltaClass}">${deltaText}</div>
      ${stHtml}
    </div>
    <div class="kpi">
      <div class="label">电表总用电量</div>
      <div class="value">${fmt(last.total_usage)}</div>
      <div class="delta flat">kWh</div>
    </div>
    <div class="kpi">
      <div class="label">采样点数</div>
      <div class="value">${data.length}</div>
      <div class="delta flat">当前窗口</div>
    </div>
    <div class="kpi">
      <div class="label">最近更新</div>
      <div class="value updated">${fullTs(last.ts)}</div>
      <div class="delta flat"></div>
    </div>`;
}

function renderRoomKpis(holder, groups) {
  holder.className = "kpis rooms";
  document.getElementById("room-sub").textContent = `全部宿舍 · ${groups.length} 间`;
  holder.innerHTML = "";
  groups.forEach((g, i) => {
    const last = g.rows[g.rows.length - 1];
    let deltaText = "", deltaClass = "flat";
    if (g.rows.length >= 2) {
      const prev = g.rows[g.rows.length - 2].surplus_charge;
      const cur = last.surplus_charge;
      if (cur !== null && prev !== null) {
        const d = cur - prev;
        deltaClass = Math.abs(d) < 1e-9 ? "flat" : (d > 0 ? "up" : "down");
        deltaText = (d > 0 ? "+" : "") + fmt(d) + " kWh";
      }
    }
    const st = balanceStatus(last.surplus_charge);
    const card = document.createElement("div");
    card.className = "kpi room";
    card.style.setProperty("--accent", roomColorFor(g.rows[0]));
    card.title = "点击放大查看该宿舍曲线";
    card.addEventListener("click", () => focusRoom(g));
    card.innerHTML = `
      <div class="label" title="${esc(g.label)}">${esc(g.label)}</div>
      <div class="value hero">${fmt(last.surplus_charge)}<span class="unit">&nbsp;kWh</span></div>
      <div class="delta ${deltaClass}">${deltaText}</div>
      ${st.level ? `<div class="status ${st.level}"><span class="dot"></span>${st.text}</div>` : ""}`;
    holder.appendChild(card);
  });
}

/* ============ ECharts:图表 ============ */
function isDark() {
  const t = document.documentElement.getAttribute("data-theme");
  return t ? t === "dark" : matchMedia("(prefers-color-scheme: dark)").matches;
}
function themeColors() {
  const dark = isDark();
  return {
    dark,
    text:    dark ? "#c3c2b7" : "#52514e",
    faint:   "#898781",
    grid:    dark ? "#2c2c2a" : "#e1e0d9",
    axis:    dark ? "#383835" : "#c3c2b7",
    surface: dark ? "#1a1a19" : "#fcfcfb",
    border:  dark ? "rgba(255,255,255,0.12)" : "rgba(11,11,11,0.12)",
  };
}
const hexA = (hex, a) => {
  const n = parseInt(hex.slice(1), 16);
  return `rgba(${n >> 16 & 255},${n >> 8 & 255},${n & 255},${a})`;
};
function xAxisFormatter(ms, st) {
  const d = new Date(ms);
  const pad = n => String(n).padStart(2, "0");
  const day = `${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
  const time = `${pad(d.getHours())}:${pad(d.getMinutes())}`;
  if (day === st.last) return time;   // 同一天只显示时刻,跨天显示日期
  st.last = day;
  return day;
}

const CHARTS = [];
function disposeCharts() {
  CHARTS.forEach(c => { try { if (!c.isDisposed()) c.dispose(); } catch (e) {} });
  CHARTS.length = 0;
}
let resizeTimer = null;
window.addEventListener("resize", () => {
  clearTimeout(resizeTimer);
  resizeTimer = setTimeout(() => {
    CHARTS.forEach(c => { try { if (!c.isDisposed()) c.resize(); } catch (e) {} });
  }, 120);
});

function buildChartOption(rows, color, mini) {
  const t = themeColors();
  const xState = { last: "" };   // 每个图表独立的跨天标签状态
  const pts = rows.filter(r => r.surplus_charge !== null && !isNaN(r.surplus_charge))
                  .map(r => [new Date(r.ts.replace(" ", "T")).getTime(), r.surplus_charge]);
  // 纵轴小数位:数值跨度小还显示整数没意义,按跨度自适应(0/1/2 位)
  const yVals = pts.map(p => p[1]);
  const ySpan = yVals.length ? Math.max(...yVals) - Math.min(...yVals) : 0;
  const yFrac = ySpan < 1 ? 2 : (ySpan < 5 ? 1 : 0);
  return {
    animation: false,
    grid: { top: 16, right: mini ? 14 : 64, bottom: 24, left: mini ? 42 : 56 },
    tooltip: {
      trigger: "axis", confine: true,
      backgroundColor: t.surface, borderColor: t.border, borderWidth: 1,
      textStyle: { color: t.text, fontSize: 12 },
      formatter: params => {
        const p = params && params[0];
        if (!p) return "";
        const raw = p.value;
        const ms = Array.isArray(raw) ? raw[0] : p.axisValue;
        const v = Array.isArray(raw) ? raw[1] : raw;
        const d = new Date(ms);
        const pad = n => String(n).padStart(2, "0");
        const date = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
        return `<span style="color:${color}">●</span> ${date}<br>剩余电量 <b>${fmt(v)} kWh</b>`;
      },
    },
    xAxis: {
      type: "time",
      axisLine: { lineStyle: { color: t.axis } },
      axisTick: { show: false },
      axisLabel: { color: t.faint, fontSize: 11, hideOverlap: true,
                   formatter: ms => xAxisFormatter(ms, xState) },
      splitLine: { show: false },
    },
    yAxis: {
      type: "value", scale: true,
      axisLabel: {
        color: t.faint, fontSize: 11,
        formatter: v => v.toLocaleString("zh-CN",
          { minimumFractionDigits: yFrac, maximumFractionDigits: yFrac }),
      },
      splitLine: { lineStyle: { color: t.grid } },
    },
    series: [{
      type: "line",
      data: pts,
      smooth: true,
      showSymbol: pts.length <= 40,
      symbol: "circle",
      symbolSize: mini ? 3 : 5,
      lineStyle: { width: 2, color },
      itemStyle: { color },
      areaStyle: {
        color: { type: "linear", x: 0, y: 0, x2: 0, y2: 1,
                 colorStops: [{ offset: 0, color: hexA(color, 0.20) },
                              { offset: 1, color: hexA(color, 0.02) }] },
      },
      ...(mini ? {} : {
        endLabel: {
          show: true, color, fontSize: 12, fontWeight: 600,
          formatter: p => fmt(p.value[1]) + " kWh",
        },
      }),
    }],
  };
}

function renderChart() {
  const holder = document.getElementById('chart-holder');
  const hint = document.getElementById('chart-hint');
  disposeCharts();
  holder.textContent = '';
  if (!state.data.length) {
    hint.textContent = '—';
    const div = document.createElement('div');
    div.className = 'empty'; div.textContent = '暂无数据'; holder.appendChild(div);
    return;
  }
  const groups = state.groups.filter(g =>
    g.rows.some(r => r.surplus_charge !== null && !isNaN(r.surplus_charge)));
  if (!groups.length) {
    hint.textContent = '—';
    const div = document.createElement('div');
    div.className = 'empty'; div.textContent = '暂无有效数据'; holder.appendChild(div);
    return;
  }
  if (typeof echarts === "undefined") {
    hint.textContent = "—";
    const div = document.createElement("div");
    div.className = "empty"; div.textContent = "图表库加载失败";
    holder.appendChild(div);
    return;
  }
  if (groups.length === 1) {
    hint.textContent = '剩余电量曲线(kWh)';
    const el = document.createElement('div');
    el.className = 'chart-main'; holder.appendChild(el);
    const ch = echarts.init(el);
    ch.setOption(buildChartOption(groups[0].rows, roomColorFor(groups[0].rows[0]), false));
    CHARTS.push(ch);
  } else {
    hint.textContent = '每间宿舍单独一张图 · 点击卡片放大';
    const grid = document.createElement('div');
    grid.className = 'mini-grid';
    const subs = [];
    groups.forEach((g, i) => {
      const box = document.createElement('div');
      box.className = 'mini-card';
      box.title = '点击放大查看该宿舍曲线';
      box.addEventListener('click', () => focusRoom(g));
      const hd = document.createElement('div');
      hd.className = 'mini-hd';
      const dot = document.createElement('span');
      dot.className = 'mini-dot'; dot.style.background = roomColorFor(g.rows[0]);
      const nm = document.createElement('span');
      nm.className = 'mini-name'; nm.title = g.label; nm.textContent = g.label;
      const last = g.rows[g.rows.length - 1];
      const val = document.createElement('span');
      val.className = 'mini-val'; val.textContent = fmt(last.surplus_charge) + ' kWh';
      hd.append(dot, nm, val);
      const sub = document.createElement('div');
      sub.className = 'chart-holder sub';
      box.append(hd, sub);
      grid.appendChild(box);
      subs.push({ sub, rows: g.rows, color: roomColorFor(g.rows[0]) });
    });
    holder.appendChild(grid);
    // 先挂到文档再初始化 ECharts,否则容器尺寸为 0,canvas 画不出来
    subs.forEach(({ sub, rows, color }) => {
      const ch = echarts.init(sub);
      ch.setOption(buildChartOption(rows, color, true));
      CHARTS.push(ch);
    });
  }
}

function renderTable() {
  const data = state.data;
  const wrap = document.getElementById("table-wrap");
  const hint = document.getElementById("table-hint");
  const multi = state.groups.length > 1;
  const pageSize = state.tablePageSize;
  const pageCount = Math.max(1, Math.ceil(data.length / pageSize));
  state.tablePage = Math.min(Math.max(1, state.tablePage), pageCount);
  hint.textContent = data.length ? `${data.length} 条 · 第 ${state.tablePage}/${pageCount} 页` : "—";
  const prev = document.getElementById("table-prev");
  const next = document.getElementById("table-next");
  const pageInfo = document.getElementById("table-page-info");
  if (prev) prev.disabled = state.tablePage <= 1;
  if (next) next.disabled = state.tablePage >= pageCount || !data.length;
  if (pageInfo) pageInfo.textContent = data.length ? `${state.tablePage} / ${pageCount}` : "0 / 0";
  if (!data.length) {
    wrap.innerHTML = '<div class="empty">暂无读数</div>';
    return;
  }
  const table = document.createElement("table");
  const thead = document.createElement("thead");
  const hr = document.createElement("tr");
  const headers = multi
    ? ["宿舍", "时间", "剩余电量(kWh)", "总用电量(kWh)", "明细"]
    : ["时间", "剩余电量(kWh)", "总用电量(kWh)", "明细"];
  headers.forEach(h => {
    const th = document.createElement("th");
    if (h.endsWith("(kWh)")) th.style.textAlign = "right";
    th.textContent = h;
    hr.appendChild(th);
  });
  thead.appendChild(hr);
  table.appendChild(thead);

  const tbody = document.createElement("tbody");
  const newestFirst = data.slice().reverse();
  const start = (state.tablePage - 1) * pageSize;
  for (const d of newestFirst.slice(start, start + pageSize)) {
    const tr = document.createElement("tr");
    if (multi) {
      const tdR = document.createElement("td");
      tdR.className = "col-room";
      const dot = document.createElement("span");
      dot.className = "room-dot";
      dot.style.background = state.colorByKey[roomKey(d)] || "var(--ink-3)";
      tdR.appendChild(dot);
      tdR.appendChild(document.createTextNode(labelForRow(d)));
      tr.appendChild(tdR);
    }
    const tdT = document.createElement("td"); tdT.textContent = fullTs(d.ts);
    const tdS = document.createElement("td");
    const v = d.surplus_charge;
    tdS.className = "num";
    tdS.textContent = fmt(v);
    if (v !== null && v < 0) tdS.classList.add("neg");
    const tdU = document.createElement("td");
    tdU.className = "num";
    tdU.textContent = fmt(d.total_usage);
    const tdD = document.createElement("td");
    tdD.textContent = Object.entries(d.show || {})
      .map(([k, v2]) => `${k}=${v2}`).join("  ") || "—";
    tr.append(tdT, tdS, tdU, tdD);
    tbody.appendChild(tr);
  }
  table.appendChild(tbody);
  wrap.replaceChildren(table);
}

/* ============ 筛选 ============ */
function initFilters() {
  document.querySelectorAll("#filters button[data-days]").forEach(btn => {
    btn.addEventListener("click", () => {
      document.querySelectorAll("#filters button[data-days]").forEach(b => b.classList.remove("active"));
      btn.classList.add("active");
      state.days = parseInt(btn.dataset.days, 10);
      state.tablePage = 1;
      refresh();
    });
  });
}

function initTablePagination() {
  const size = document.getElementById("table-page-size");
  size.value = String(state.tablePageSize);
  size.addEventListener("change", () => {
    state.tablePageSize = parseInt(size.value, 10) || 10;
    state.tablePage = 1;
    renderTable();
  });
  document.getElementById("table-prev").addEventListener("click", () => {
    if (state.tablePage > 1) {
      state.tablePage--;
      renderTable();
    }
  });
  document.getElementById("table-next").addEventListener("click", () => {
    if (state.tablePage * state.tablePageSize < state.data.length) {
      state.tablePage++;
      renderTable();
    }
  });
}

/* ============ 宿舍选择(切数据) ============ */
function matchRoom(a, b) {
  return !!(a && b && a.campus === b.campus && a.building === b.building && a.room === b.room);
}

function focusRoom(g) {
  const r = g.rows[0];
  navigate({ campus: r.campus, building: r.building, room: r.room });
}

function initBackButton() {
  document.getElementById("back-btn").addEventListener("click", () => {
    if (!state.room) return;
    navigate(null);   // 返回全部宿舍全览
  });
}

/* ============ 查询设置 ============ */
let draftTargets = [];
let pickCampus = null, pickBuilding = null, pickRoom = null;
let campusRequestSeq = 0, buildingRequestSeq = 0, roomRequestSeq = 0;
let targetLookupSeq = 0;
let pickedTargetLookup = { key: "", loading: false, exists: false, hidden: false, error: null };
const ADMIN_KEY_SESSION = "elec-admin-key";
let adminKey = sessionStorage.getItem(ADMIN_KEY_SESSION) || "";
// 允许通过 /?key=... 直接打开管理页面；读取后立即从地址栏移除，避免密钥留在历史记录/复制链接中。
try {
  const directURL = new URL(location.href);
  const keyFromURL = directURL.searchParams.get("key");
  if (keyFromURL && keyFromURL.trim()) {
    adminKey = keyFromURL.trim();
    sessionStorage.setItem(ADMIN_KEY_SESSION, adminKey);
    directURL.searchParams.delete("key");
    const clean = directURL.pathname + directURL.search + directURL.hash;
    history.replaceState(history.state, "", clean);
  }
} catch (_) {}
let adminPromptResolver = null;

function initAdminPrompt() {
  const modal = document.getElementById("admin-modal");
  const input = document.getElementById("admin-key-input");
  const finish = value => {
    if (modal.classList.contains("home-gate") && !value) return;
    modal.hidden = true;
    const resolve = adminPromptResolver;
    adminPromptResolver = null;
    if (resolve) resolve(value);
  };
  document.getElementById("admin-submit").addEventListener("click", () => finish(input.value.trim()));
  document.getElementById("admin-cancel").addEventListener("click", () => finish(""));
  input.addEventListener("keydown", event => {
    if (event.key === "Enter") { event.preventDefault(); finish(input.value.trim()); }
    if (event.key === "Escape" && !modal.classList.contains("home-gate")) { event.preventDefault(); finish(""); }
  });
  modal.addEventListener("click", event => {
    if (event.target === modal && !modal.classList.contains("home-gate")) finish("");
  });
}

function promptAdminKey(required = false) {
  if (adminPromptResolver) return Promise.resolve("");
  const modal = document.getElementById("admin-modal");
  const input = document.getElementById("admin-key-input");
  input.value = "";
  modal.classList.toggle("home-gate", required);
  document.getElementById("admin-cancel").hidden = required;
  document.getElementById("admin-title").textContent = required ? "主页访问验证" : "管理鉴权";
  document.getElementById("admin-prompt-hint").textContent = required
    ? "主页已隐藏，请输入管理鉴权密钥后加载主页和数据。"
    : "密钥只保存在当前浏览器标签页，关闭标签页后失效。";
  modal.hidden = false;
  setTimeout(() => input.focus(), 0);
  return new Promise(resolve => { adminPromptResolver = resolve; });
}

async function adminFetch(url, options = {}) {
  const { forceAdminKey = false, ...fetchOptions } = options;
  const headers = new Headers(fetchOptions.headers || {});
  if (adminKey && (forceAdminKey || state.adminAuthRequired || state.homepageUnlocked)) {
    headers.set("Authorization", `Bearer ${adminKey}`);
  }
  const response = await fetch(url, { ...fetchOptions, headers, cache: fetchOptions.cache || "no-store" });
  if (response.status === 401) {
    adminKey = "";
    sessionStorage.removeItem(ADMIN_KEY_SESSION);
  }
  return response;
}

async function responseJSON(response) {
  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    const error = new Error(body.error || `HTTP ${response.status}`);
    error.status = response.status;
    throw error;
  }
  return body;
}

async function ensureAdmin() {
  if (!state.adminAuthRequired) return true;
  return ensureAdminKey(false);
}

async function ensureAdminKey(required) {
  if (adminKey) {
    try {
      await responseJSON(await adminFetch("/api/admin/verify", { method: "POST", forceAdminKey: true }));
      return true;
    } catch (e) {
      if (adminKey && !required) throw e;
    }
  }
  while (true) {
    const entered = await promptAdminKey(required);
    if (!entered) return false;
    adminKey = entered.trim();
    try {
      await responseJSON(await adminFetch("/api/admin/verify", { method: "POST", forceAdminKey: true }));
      sessionStorage.setItem(ADMIN_KEY_SESSION, adminKey);
      return true;
    } catch (e) {
      adminKey = "";
      sessionStorage.removeItem(ADMIN_KEY_SESSION);
      if (!required) throw e;
      toast("管理密钥无效，请重新输入", true);
    }
  }
}

async function unlockHomepage() {
  if (state.room || state.showHomepage || state.homepageUnlocked) return true;
  const unlocked = await ensureAdminKey(true);
  if (!unlocked) return false;
  state.homepageUnlocked = true;
  document.body.classList.add("homepage-unlocked");
  return true;
}

function discoveryParams(extra = {}) {
  return new URLSearchParams(extra);
}

function fillSelect(el, items, placeholder, disabled) {
  el.textContent = "";
  const ph = document.createElement("option");
  ph.value = ""; ph.textContent = placeholder;
  el.appendChild(ph);
  (items || []).forEach(it => {
    const o = document.createElement("option");
    o.value = it.value;
    o.dataset.name = it.name;
    o.textContent = it.name;
    el.appendChild(o);
  });
  el.disabled = !!disabled;
}

async function loadCampuses() {
  return responseJSON(await adminFetch(`/api/campuses?${discoveryParams()}`));
}

async function loadBuildings(campus) {
  return responseJSON(await adminFetch(`/api/buildings?${discoveryParams({ campus })}`));
}

async function loadRooms(campus, building) {
  return responseJSON(await adminFetch(`/api/rooms?${discoveryParams({ campus, building })}`));
}

function resetPicker() {
  pickCampus = pickBuilding = pickRoom = null;
  resetTargetLookup();
  const requestSeq = ++campusRequestSeq;
  buildingRequestSeq++;
  roomRequestSeq++;
  fillSelect(document.getElementById("pick-campus"), [], "— 加载校区 —", true);
  fillSelect(document.getElementById("pick-building"), [], "— 选择楼栋 —", true);
  fillSelect(document.getElementById("pick-room"), [], "— 选择房间 —", true);
  document.getElementById("pick-add").disabled = true;
  document.getElementById("pick-preview").textContent = "";
  document.getElementById("pick-tag").value = "";
  loadCampuses().then(items => {
    if (requestSeq !== campusRequestSeq) return;
    fillSelect(document.getElementById("pick-campus"), items, "— 选择校区 —", false);
  }).catch(e => {
    if (requestSeq !== campusRequestSeq) return;
    fillSelect(document.getElementById("pick-campus"), [], "— 选择校区 —", false);
    document.getElementById("pick-preview").textContent = "加载校区失败: " + e.message;
  });
}

function selectedTargetKey() {
  return pickCampus && pickBuilding && pickRoom
    ? `${pickCampus.value}|${pickBuilding.value}|${pickRoom.value}`
    : "";
}

function resetTargetLookup() {
  targetLookupSeq++;
  pickedTargetLookup = { key: "", loading: false, exists: false, hidden: false, error: null };
}

async function lookupPickedTarget() {
  const key = selectedTargetKey();
  if (!key) {
    resetTargetLookup();
    updatePreview();
    return;
  }
  const seq = ++targetLookupSeq;
  pickedTargetLookup = { key, loading: true, exists: false, hidden: false, error: null };
  updatePreview();
  const params = new URLSearchParams({ campus: pickCampus.value, building: pickBuilding.value, room: pickRoom.value });
  try {
    const result = await responseJSON(await adminFetch(`/api/config?${params.toString()}`));
    if (seq !== targetLookupSeq || selectedTargetKey() !== key) return;
    if (typeof result.target_exists !== "boolean") throw new Error("宿舍查询结果不可用");
    pickedTargetLookup = {
      key, loading: false, exists: result.target_exists,
      hidden: result.target_exists && result.target_hidden === true, error: null,
    };
  } catch (error) {
    if (seq !== targetLookupSeq || selectedTargetKey() !== key) return;
    pickedTargetLookup = { key, loading: false, exists: false, hidden: false, error };
  }
  updatePreview();
}

function updatePreview() {
  const preview = document.getElementById("pick-preview");
  const addBtn = document.getElementById("pick-add");
  const label = pickCampus && pickBuilding && pickRoom
    ? `${pickCampus.name}/${pickBuilding.name}/${pickRoom.name}` : "";
  const lookup = pickedTargetLookup;
  addBtn.disabled = true;
  addBtn.textContent = "添加";
  if (!label) {
    preview.textContent = "";
  } else if (lookup.loading || lookup.key !== selectedTargetKey()) {
    preview.textContent = "正在查询宿舍…";
  } else if (lookup.error) {
    preview.textContent = "查询失败: " + lookup.error.message;
  } else if (lookup.exists) {
    preview.textContent = lookup.hidden ? "宿舍已存在（进入后自动解除隐藏）" : "宿舍已存在";
    addBtn.textContent = "查看宿舍";
    addBtn.disabled = false;
  } else {
    preview.textContent = `将添加: ${label}`;
    addBtn.disabled = false;
  }
}

function renderTargetList() {
  const wrap = document.getElementById("target-list");
  wrap.textContent = "";
  if (!draftTargets.length) {
    const div = document.createElement("div");
    div.className = "empty-hint";
    div.textContent = "还没有监控宿舍,在下面添加。";
    wrap.appendChild(div);
    return;
  }
  draftTargets.forEach(t => {
    const item = document.createElement("div");
    item.className = "target-item";
    const name = document.createElement("span");
    name.className = "tl-name";
    name.textContent = targetLabel(t);
    const id = document.createElement("span");
    id.className = "tl-id";
    id.textContent = `${t.campus}/${t.building}/${t.room}`;
    item.append(name, id);
    wrap.appendChild(item);
  });
}

function settingsAdditionOnly() {
  return !!state.room && !state.adminAuthRequired && !state.showHomepage;
}

async function openSettings() {
  if (!await ensureAdmin()) return;
  const additionOnly = settingsAdditionOnly();
  document.getElementById("target-section").hidden = additionOnly;
  if (additionOnly) {
    draftTargets = [];
    document.getElementById("target-list").textContent = "";
  } else {
    const cfg = await responseJSON(await adminFetch("/api/config?admin=1"));
    draftTargets = (cfg.targets || []).map(t => ({ ...t }));
    renderTargetList();
  }
  resetPicker();
  document.getElementById("settings-modal").hidden = false;
}

function initSettings() {
  const modal = document.getElementById("settings-modal");
  document.getElementById("settings-btn").addEventListener("click", () => {
    openSettings().catch(e => toast("打开设置失败: " + e.message, true));
  });
  document.getElementById("modal-close").addEventListener("click", () => { modal.hidden = true; });
  modal.addEventListener("click", e => { if (e.target === modal) modal.hidden = true; });

  const selC = document.getElementById("pick-campus");
  const selB = document.getElementById("pick-building");
  const selR = document.getElementById("pick-room");

  selC.addEventListener("change", async e => {
    const opt = e.target.selectedOptions[0];
    pickCampus = opt.value ? { value: opt.value, name: opt.dataset.name } : null;
    pickBuilding = pickRoom = null;
    resetTargetLookup();
    const requestSeq = ++buildingRequestSeq;
    roomRequestSeq++;
    fillSelect(selB, [], "— 选择楼栋 —", true);
    fillSelect(selR, [], "— 选择房间 —", true);
    document.getElementById("pick-tag").value = "";
    updatePreview();
    if (pickCampus) {
      const campusValue = pickCampus.value;
      try {
        const items = await loadBuildings(campusValue);
        if (requestSeq !== buildingRequestSeq || !pickCampus || pickCampus.value !== campusValue) return;
        fillSelect(selB, items, "— 选择楼栋 —", false);
      } catch (err) {
        if (requestSeq !== buildingRequestSeq) return;
        document.getElementById("pick-preview").textContent = "加载楼栋失败: " + err.message;
      }
    }
  });
  selB.addEventListener("change", async e => {
    const opt = e.target.selectedOptions[0];
    pickBuilding = opt.value ? { value: opt.value, name: opt.dataset.name } : null;
    pickRoom = null;
    resetTargetLookup();
    const requestSeq = ++roomRequestSeq;
    fillSelect(selR, [], "— 选择房间 —", true);
    document.getElementById("pick-tag").value = "";
    updatePreview();
    if (pickBuilding) {
      const campusValue = pickCampus.value;
      const buildingValue = pickBuilding.value;
      try {
        const items = await loadRooms(campusValue, buildingValue);
        if (requestSeq !== roomRequestSeq || !pickBuilding || pickBuilding.value !== buildingValue) return;
        fillSelect(selR, items, "— 选择房间 —", false);
      } catch (err) {
        if (requestSeq !== roomRequestSeq) return;
        document.getElementById("pick-preview").textContent = "加载房间失败: " + err.message;
      }
    }
  });
  selR.addEventListener("change", e => {
    const opt = e.target.selectedOptions[0];
    pickRoom = opt.value ? { value: opt.value, name: opt.dataset.name } : null;
    document.getElementById("pick-tag").value = pickRoom ? pickRoom.name : "";
    updatePreview();
    if (pickRoom) lookupPickedTarget().catch(() => {});
  });

  document.getElementById("pick-add").addEventListener("click", async () => {
    if (!(pickCampus && pickBuilding && pickRoom)) return;
    const key = selectedTargetKey();
    const lookup = pickedTargetLookup;
    if (lookup.key !== key || lookup.loading || lookup.error) return;
    const t = {
      feeitemid: state.defaults.feeitemid, appId: state.defaults.appId,
      campus: pickCampus.value, building: pickBuilding.value, room: pickRoom.value,
      label: document.getElementById("pick-tag").value.trim() || `${pickCampus.name}/${pickBuilding.name}/${pickRoom.name}`,
    };
    if (lookup.exists && !lookup.hidden) {
      modal.hidden = true;
      navigate({ campus: t.campus, building: t.building, room: t.room, label: t.label });
      return;
    }
    if (lookup.exists && lookup.hidden) t.show_in_web = true;
    const addBtn = document.getElementById("pick-add");
    addBtn.disabled = true;
    try {
      const body = await responseJSON(await adminFetch("/api/config", {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ target: t }),
      }));
      if (lookup.exists && lookup.hidden) {
        modal.hidden = true;
        navigate({ campus: t.campus, building: t.building, room: t.room, label: t.label });
      } else if (settingsAdditionOnly()) {
        modal.hidden = true;
        navigate({ campus: t.campus, building: t.building, room: t.room, label: t.label });
        await refreshPublicConfig();
      } else {
        draftTargets = (body.targets || []).map(x => ({ ...x }));
        renderTargetList();
        document.getElementById("pick-preview").textContent = "已添加并自动保存";
        await refreshPublicConfig();
        await refresh();
      }
    } catch (e) {
      document.getElementById("pick-preview").textContent = "保存失败: " + e.message;
    } finally {
      resetTargetLookup();
      fillSelect(selB, [], "— 选择楼栋 —", true);
      fillSelect(selR, [], "— 选择房间 —", true);
      pickBuilding = pickRoom = null;
      document.getElementById("pick-tag").value = "";
    }
  });
}

/* ============ 立即采集 ============ */
function toast(msg, isErr) {
  const t = document.getElementById("toast");
  t.textContent = msg;
  t.classList.toggle("err", !!isErr);
  t.classList.add("show");
  clearTimeout(t._t);
  t._t = setTimeout(() => t.classList.remove("show"), 3200);
}

/* 立即采集只能作用于"当前配置里的监控目标"。state.room 是钻取时从数据行构造的,
   可能命中 config 之外的宿舍(历史/已移除/IDs 已变)——不把必失败的请求发给后端,
   直接提示。返回要采集的 config target,无则 null(已 toast)。 */
function collectTarget() {
  if (state.room) {
    const matched = state.targets.find(x => matchRoom(x, state.room));
    if (matched) return matched;
    toast("该宿舍不在监控列表中,无法立即采集。可在「查询设置」里添加", true);
    return null;
  }
  if (!state.targets.length) {
    toast("还没有监控宿舍,先到「查询设置」添加", true);
    return null;
  }
  return state.targets[0];
}

function collectAllProgress(d) {
  const btn = document.getElementById("collect-btn");
  const cancel = document.getElementById("collect-cancel-btn");
  if (["done", "failed", "cancelled"].includes(d.state)) {
    if (!activeJobId || d.job_id === activeJobId) setActiveJobId("");
    cancel.hidden = true;
    resetCollectButton(btn, "⚡ 立即采集");
    return;
  }
  if (d.state === "cancelling") {
    cancel.hidden = true;
    btn.disabled = true;
    btn.classList.add("busy");
    btn.textContent = "正在取消…";
    return;
  }
  cancel.hidden = false;
  const total = d.requested || 0;
  const done = d.completed || 0;
  const pct = Math.max(0, Math.min(100, Number(d.percent ?? (total ? done * 100 / total : 0))));
  btn.style.setProperty("--progress", `${pct}%`);
  btn.classList.add("busy");
  btn.textContent = total ? `采集 ${done}/${total}` : "准备采集…";
  if (d.current && d.current.label) btn.title = `正在采集: ${d.current.label}`;
}

function resetCollectButton(btn, text) {
  btn.disabled = false;
  btn.classList.remove("busy");
  btn.style.setProperty("--progress", "0%");
  btn.textContent = text;
  btn.removeAttribute("title");
}

async function pollCollectAll(jobId, btn, oldText) {
  while (true) {
    const d = await responseJSON(await adminFetch(`/api/collect-all/status?job_id=${encodeURIComponent(jobId)}`));
    collectAllProgress(d);
    if (["done", "failed", "cancelled"].includes(d.state)) return d;
    await new Promise(resolve => setTimeout(resolve, 800));
  }
}

async function collectAllWithProgress(btn, oldText) {
  const job = await responseJSON(await adminFetch("/api/collect-all", {
    method: "POST", headers: { "Content-Type": "application/json" },
  }));
  setActiveJobId(job.job_id);
  const result = await pollCollectAll(job.job_id, btn, oldText);
  if (result.state === "cancelled") {
    toast(`采集已取消，已完成 ${result.completed || 0}/${result.requested || 0}`);
    await refresh();
    return;
  }
  if (result.state === "failed") {
    toast("批量采集失败: " + (result.error || "未知错误"), true);
    await refresh();
    return;
  }
  const failed = result.failed || (result.results || []).filter(r => r.error).length;
  const all = result.state === "done" && !failed && result.success === result.requested;
  const parts = [`成功 ${result.success || 0}/${result.requested || 0}`];
  if (failed) parts.push(`${failed} 间失败`);
  toast(all ? `已采集全部 ${result.success} 间` : "采集结果: " + parts.join(","), !all);
  await refresh();
}

async function collectNow() {
  const btn = document.getElementById("collect-btn");
  const old = "⚡ 立即采集";
  try {
    if (!await ensureAdmin()) return;
  } catch (e) {
    toast("管理鉴权失败: " + e.message, true);
    return;
  }
  btn.disabled = true;
  btn.classList.add("busy");
  btn.style.setProperty("--progress", "0%");
  if (!state.room) {
    if (!state.targets.length) {
      resetCollectButton(btn, old);
      toast("还没有监控宿舍,先到「查询设置」添加", true);
      return;
    }
    collectAllWithProgress(btn, old)
      .catch(e => toast("采集失败: " + e.message, true))
      .finally(() => resetCollectButton(btn, old));
    return;
  }
  const t = collectTarget();
  if (!t) { resetCollectButton(btn, old); return; }
  btn.textContent = "采集中…";
  adminFetch("/api/collect", {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ campus: t.campus, building: t.building, room: t.room }),
  })
    .then(r => r.json().catch(() => ({ error: `HTTP ${r.status}` })))
    .then(d => {
      if (d && d.ok) toast(`已采集 ${d.display_label || d.room_label}:剩余 ${fmt(d.surplus_charge)} kWh`);
      else toast("采集失败:" + ((d && d.error) || "未知错误"), true);
      refresh();
    })
    .catch(e => toast("采集失败:" + e.message, true))
    .finally(() => resetCollectButton(btn, old));
}

async function resumeCollectJob() {
  if (!activeJobId || (state.adminAuthRequired && !adminKey)) return;
  const jobId = activeJobId;
  const btn = document.getElementById("collect-btn");
  btn.disabled = true;
  collectAllProgress({ state: "queued", job_id: jobId });
  try {
    const result = await pollCollectAll(jobId, btn, "⚡ 立即采集");
    if (result.state === "cancelled") {
      toast(`采集已取消，已完成 ${result.completed || 0}/${result.requested || 0}`);
    } else if (result.state === "failed") {
      toast("批量采集失败: " + (result.error || "未知错误"), true);
    } else {
      toast(`批量采集已完成，成功 ${result.success || 0}/${result.requested || 0}`,
            !!result.failed);
    }
    await refresh();
  } catch (e) {
    if (e.status === 404) setActiveJobId("");
    toast("恢复批量任务状态失败: " + e.message, true);
  } finally {
    resetCollectButton(btn, "⚡ 立即采集");
  }
}

/* ============ 启动 ============ */
async function boot() {
  state.room = roomFromPath();
  state.showHomepage = document.body.dataset.showHomepage !== "false";

  // 服务端已把主页开关写入页面，关闭时先验证，绝不预载缓存或读数。
  if (!state.room && !state.showHomepage) await unlockHomepage();

  try {
    await refreshPublicConfig();
    if (!state.room && !state.showHomepage) await unlockHomepage();
    await refresh();
  } catch (e) {
    console.warn('网络加载失败,尝试使用缓存数据:', e);
    if ('caches' in window) {
      try {
        const cachedConfig = await caches.match('/api/config');
        if (cachedConfig) {
          const cfg = await cachedConfig.json();
          if (cfg && cfg.targets) state.targets = cfg.targets;
          if (cfg && cfg.defaults) state.defaults = cfg.defaults;
          if (cfg) state.showHomepage = cfg.show_homepage !== false;
        }
        if (!state.room && !state.showHomepage) await unlockHomepage();
        const cachedReadings = await caches.match(readingsURL());
        if (cachedReadings) {
          const data = await cachedReadings.json();
          if (Array.isArray(data)) {
            state.data = data;
            render();
          }
        }
      } catch (cacheError) { console.warn('缓存加载失败:', cacheError); }
    }
  }

  // 连接 SSE 实时推送
  connectSSE();
  startConfigPolling();
  resumeCollectJob();
}
initTheme();
initFilters();
initBackButton();
initTablePagination();
initAdminPrompt();
initSettings();
document.getElementById("collect-btn").addEventListener("click", collectNow);
document.getElementById("collect-cancel-btn").addEventListener("click", async function() {
  const btn = document.getElementById("collect-cancel-btn");
  btn.disabled = true;
  try {
    if (!await ensureAdmin()) return;
    if (!activeJobId) throw new Error("当前任务 ID 不可用");
    const d = await responseJSON(await adminFetch(
      `/api/collect-all/cancel?job_id=${encodeURIComponent(activeJobId)}`,
      { method: "POST" },
    ));
    if (d.ok) {
      toast("取消请求已发送，正在等待当前请求退出");
      collectAllProgress({ state: "cancelling", job_id: activeJobId });
    }
  } catch (e) {
    toast("取消失败: " + e.message, true);
  } finally {
    btn.disabled = false;
    btn.hidden = true;
  }
});
boot();   // 含 SSE 连接 + 缓存优先加载
