const CACHE_NAME = 'elec-monitor-v3';
const STATIC_ASSETS = [
    '/404.html',
    '/offline.html',
    '/manifest.json',
];

// 安装: 预缓存静态资源
self.addEventListener('install', event => {
    event.waitUntil(
        caches.open(CACHE_NAME).then(cache => cache.addAll(STATIC_ASSETS))
    );
    self.skipWaiting();
});

// 激活: 清理旧缓存
self.addEventListener('activate', event => {
    event.waitUntil(
        caches.keys().then(keys =>
            Promise.all(keys.filter(k => k !== CACHE_NAME).map(k => caches.delete(k)))
        )
    );
    self.clients.claim();
});

// 拦截请求
self.addEventListener('fetch', event => {
    const url = new URL(event.request.url);

    // SSE 端点不缓存
    if (url.pathname === '/api/events') return;

    // 带版本号的静态资源: Cache First
    if (url.pathname.startsWith('/static/')) {
        event.respondWith(cacheFirst(event.request));
        return;
    }

    // API 请求: Network First, fallback to cache
    if (url.pathname.startsWith('/api/')) {
        event.respondWith(networkFirst(event.request));
        return;
    }

    // 页面(含 / 与 /room/...): Network First,离线回退 -> 部署后自动生效不陈旧
    if (event.request.mode === 'navigate') {
        event.respondWith(networkFirst(event.request, '/offline.html'));
        return;
    }

    // 其它(manifest/404 等): Network First, fallback to cache
    event.respondWith(networkFirst(event.request));
});

async function cacheFirst(request) {
    const cached = await caches.match(request);
    return cached || fetch(request);
}

async function networkFirst(request, fallbackUrl) {
    try {
        const response = await fetch(request);
        if (response.ok) {
            const cache = await caches.open(CACHE_NAME);
            cache.put(request, response.clone());
        }
        return response;
    } catch (err) {
        const cached = await caches.match(request);
        if (cached) return cached;
        if (fallbackUrl) return caches.match(fallbackUrl);
        throw err;
    }
}