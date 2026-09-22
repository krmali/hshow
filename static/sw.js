// Minimal passthrough service worker — required for iOS PWA installation.
// No caching: every request goes to the network so the dashboard is always fresh.
self.addEventListener('fetch', function(event) {
  event.respondWith(fetch(event.request));
});
