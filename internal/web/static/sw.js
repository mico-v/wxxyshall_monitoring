const CACHE_PREFIX = 'elec-monitor-';
// 构建时由服务端注入 git 短 hash；版本变化时 CACHE_NAME 随之变化，旧缓存自动失效。
const CACHE_VERSION = "__ELEC_VERSION__";
const CACHE_NAME = `${CACHE_PREFIX}${CACHE_VERSION}`;
const APP_SHELL = [
  '/',
  '/offline.html',
  '/404.html',
  '/manifest.json',
  '/favicon.ico',
  '/static/echarts.min.js',
  '/static/app.js',
  '/static/theme.js',
  '/static/icon-192.png',
  '/static/icon-512.png',
];

self.addEventListener('install', event => {
  event.waitUntil(caches.open(CACHE_NAME).then(cache => cache.addAll(APP_SHELL)));
  self.skipWaiting();
});

self.addEventListener('activate', event => {
  event.waitUntil(
    caches.keys().then(keys => Promise.all(
      keys.filter(key => key.startsWith(CACHE_PREFIX) && key !== CACHE_NAME)
        .map(key => caches.delete(key))
    ))
  );
  self.clients.claim();
});

self.addEventListener('fetch', event => {
  const request = event.request;
  const url = new URL(request.url);
  if (url.origin !== self.location.origin || url.pathname === '/api/events') return;

  // 管理请求和所有写请求绝不进入缓存。
  if (request.method !== 'GET' || request.headers.has('Authorization') || url.searchParams.has('key')) {
    event.respondWith(fetch(request));
    return;
  }

  if (url.pathname.startsWith('/static/') || url.pathname === '/manifest.json' || url.pathname === '/favicon.ico') {
    event.respondWith(cacheFirst(request));
    return;
  }

  if (url.pathname.startsWith('/api/')) {
    event.respondWith(networkFirst(request, null, true));
    return;
  }

  if (request.mode === 'navigate') {
    event.respondWith(appShell(event, request));
    return;
  }

  event.respondWith(networkFirst(request));
});

async function cacheFirst(request) {
  const cached = await caches.match(request);
  if (cached) return cached;
  const response = await fetch(request);
  if (response.ok) {
    const cache = await caches.open(CACHE_NAME);
    await cache.put(request, response.clone());
  }
  return response;
}

// 页面导航优先返回已预缓存的 app shell，并在后台更新 HTML。
// 这样已安装的 PWA 打开和前进/后退不再等待网络，同时下次启动能拿到新版本。
async function appShell(event, request) {
  const cache = await caches.open(CACHE_NAME);
  const cached = await cache.match(request) || await cache.match('/');
  const update = fetch(request, { cache: 'no-store' }).then(async response => {
    if (response.ok) {
      try { await cache.put('/', response.clone()); } catch (_) {}
    }
    return response;
  });

  if (cached) {
    event.waitUntil(update.catch(() => undefined));
    return cached;
  }

  try {
    return await update;
  } catch (err) {
    const offline = await cache.match('/offline.html');
    if (offline) return offline;
    throw err;
  }
}

async function networkFirst(request, fallbackUrl, apiRequest = false) {
  try {
    const response = await fetch(request);
    if (response.ok) {
      const cache = await caches.open(CACHE_NAME);
      await cache.put(request, response.clone());
    }
    return response;
  } catch (err) {
    const cached = await caches.match(request);
    if (cached) return cached;
    if (fallbackUrl) {
      const fallback = await caches.match(fallbackUrl);
      if (fallback) return fallback;
    }
    if (apiRequest) {
      return new Response(JSON.stringify({ error: '离线且没有该请求的缓存数据' }), {
        status: 503,
        headers: { 'Content-Type': 'application/json; charset=utf-8', 'Cache-Control': 'no-store' },
      });
    }
    const offline = await caches.match('/offline.html');
    if (offline) return offline;
    throw err;
  }
}
