// Fixture (plan §11.6): allocates past the container's 1 GiB limit.
// Expect the container killed by Docker without affecting other services.
const chunks = [];
setInterval(() => {
  // Buffer.alloc zeroes the memory, so the pages are really committed.
  chunks.push(Buffer.alloc(128 * 1024 * 1024));
}, 50);
