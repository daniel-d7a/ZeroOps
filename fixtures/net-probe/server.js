// Fixture (plan §11.8): tries to reach another project's container at
// TARGET_URL (set by the test). Expect reachable=false — projects are on
// separate networks and can see each other's containers.
const http = require('http');

function probe(target) {
  return new Promise((resolve) => {
    let url;
    try {
      url = new URL(target);
    } catch (err) {
      return resolve({ reachable: false, error: 'bad TARGET_URL: ' + err.message });
    }
    const req = http.get(
      { hostname: url.hostname, port: url.port || 80, path: url.pathname, timeout: 2000 },
      (res) => {
        res.resume();
        resolve({ reachable: res.statusCode === 200, status: res.statusCode });
      },
    );
    req.on('timeout', () => {
      req.destroy();
      resolve({ reachable: false, error: 'timeout' });
    });
    req.on('error', (err) => resolve({ reachable: false, error: err.code || err.message }));
  });
}

http.createServer((req, res) => {
  if (req.url !== '/probe') {
    res.writeHead(404).end();
    return;
  }
  const target = process.env.TARGET_URL;
  if (!target) {
    res.writeHead(400, { 'content-type': 'text/plain' }).end('TARGET_URL is not set');
    return;
  }
  probe(target).then((result) => {
    res.writeHead(200, { 'content-type': 'application/json' }).end(JSON.stringify(result));
  });
}).listen(process.env.PORT || 3000);
