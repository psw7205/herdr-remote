// The cache suffix below is replaced at build time (web/plugins/swBuildId.ts). A new
// deploy yields a new worker and cache; `activate` drops the previous caches.
const shell = 'herdr-chat-shell-__BUILD_ID__'
const precache = ['/', '/manifest.webmanifest', '/icon.svg', '/icon-192.png', '/icon-512.png', '/icon-512-maskable.png']

function store(request, response) {
  if (response.ok) { const clone = response.clone(); void caches.open(shell).then(cache => cache.put(request, clone)) }
  return response
}

self.addEventListener('install', event => {
  event.waitUntil(caches.open(shell).then(cache => cache.addAll(precache.map(url => new Request(url, { cache: 'reload' })))))
})
self.addEventListener('activate', event => {
  event.waitUntil(caches.keys().then(keys => Promise.all(keys.filter(key => key !== shell).map(key => caches.delete(key)))))
})
self.addEventListener('fetch', event => {
  const request = event.request
  const url = new URL(request.url)
  if (request.method !== 'GET' || url.origin !== self.location.origin || url.pathname.startsWith('/api/')) return
  if (request.mode === 'navigate') {
    event.respondWith(fetch(request).then(response => store('/', response)).catch(() => caches.match('/')))
    return
  }
  // Hashed build output never changes under the same URL.
  if (url.pathname.startsWith('/assets/')) {
    event.respondWith(caches.match(request).then(cached => cached || fetch(request).then(response => store(request, response))))
    return
  }
  // manifest, icons and other unhashed files: serve the cached copy, refresh it in the background.
  event.respondWith(caches.match(request).then(cached => {
    const network = fetch(request, { cache: 'no-cache' }).then(response => store(request, response))
    if (!cached) return network
    event.waitUntil(network.catch(() => undefined))
    return cached
  }))
})
