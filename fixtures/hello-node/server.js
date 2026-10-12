// Fixture (plan §11.1): minimal app on $PORT. Expect `live` and a working URL.
const http = require('http');

http.createServer((req, res) => {
  res.writeHead(200, { 'content-type': 'text/plain' }).end('hello from hello-node');
}).listen(process.env.PORT || 3000);
