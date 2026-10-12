// Fixture (plan §11.4): the build fails (npm install cannot resolve the
// dependency above). Expect deployment `failed`, no retries, visible logs.
const http = require('http');

http.createServer((req, res) => {
  res.writeHead(200).end('never built');
}).listen(process.env.PORT || 3000);
