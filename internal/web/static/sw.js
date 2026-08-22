const CACHE_NAME = 'elec-monitor-v1';
const STATIC_ASSETS = [
    '/',
    '/static/echarts.min.js',
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

    // 静态资源: Cache First
    if (STATIC_ASSETS.includes(url.pathname) || url.pathname.startsWith('/static/')) {
        event.respondWith(cacheFirst(event.request));
        return;
    }

    // API 请求: Network First, fallback to cache
    if (url.pathname.startsWith('/api/')) {
        event.respondWith(networkFirst(event.request));
        return;
    }

    // 导航请求: Network First, fallback to offline page
    if (event.request.mode === 'navigate') {
        event.respondWith(networkFirst(event.request, '/offline.html'));
        return;
    }
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