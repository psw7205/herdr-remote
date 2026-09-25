const shell = 'herdr-chat-shell-v2'
self.addEventListener('install', event => {
  event.waitUntil(caches.open(shell).then(cache => cache.addAll(['/', '/manifest.webmanifest', '/icon.svg', '/icon-192.png', '/icon-512.png', '/icon-512-maskable.png'])))
})
self.addEventListener('activate', event => {
  event.waitUntil(caches.keys().then(keys => Promise.all(keys.filter(key => key !== shell).map(key => caches.delete(key)))))
})
self.addEventListener('fetch', event => {
  const request = event.request
  const url = new URL(request.url)
  if (request.method !== 'GET' || url.origin !== self.location.origin || url.pathname.startsWith('/api/')) return
  if (request.mode === 'navigate') {
    event.respondWith(fetch(request).then(response => { if (response.ok) { const clone = response.clone(); caches.open(shell).then(cache => cache.put('/', clone)) } return response }).catch(() => caches.match('/')))
    return
  }
  event.respondWith(caches.match(request).then(cached => cached || fetch(request).then(response => { if (response.ok) { const clone = response.clone(); caches.open(shell).then(cache => cache.put(request, clone)) } return response })))
})
