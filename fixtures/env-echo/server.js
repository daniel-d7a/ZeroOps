// Fixture (plan §11.3): exposes GET /env?key=GREETING — set the variable,
// restart, and the new value must appear. Runs on $PORT (always 3000 for us).
const http = require('http');

http.createServer((req, res) => {
  const url = new URL(req.url, 'http://x');
  if (url.pathname !== '/env') {
    res.writeHead(404).end();
    return;
  }
  const key = url.searchParams.get('key');
  if (!key) {
    res.writeHead(400).end('key query param required');
    return;
  }
  const value = process.env[key];
  if (value === undefined) {
    res.writeHead(404).end(key + ' is not set');
    return;
  }
  res.writeHead(200, { 'content-type': 'text/plain' }).end(value);
}).listen(process.env.PORT || 3000);
