// Fixture (plan §11.2): connects with DATABASE_URL and exposes GET /db.
// Each hit records a visit — a working round trip means the app can write
// to (and read back from) its own role-isolated database.
const http = require('http');
const { Pool } = require('pg');

const pool = new Pool({ connectionString: process.env.DATABASE_URL });

http.createServer(async (req, res) => {
  if (req.url !== '/db') {
    res.writeHead(404).end();
    return;
  }
  try {
    await pool.query('CREATE TABLE IF NOT EXISTS visits (id serial PRIMARY KEY, at timestamptz NOT NULL DEFAULT now())');
    await pool.query('INSERT INTO visits DEFAULT VALUES');
    const { rows } = await pool.query('SELECT id, at FROM visits ORDER BY id DESC LIMIT 5');
    res.writeHead(200, { 'content-type': 'application/json' })
      .end(JSON.stringify({ ok: true, recent: rows }));
  } catch (err) {
    res.writeHead(500, { 'content-type': 'application/json' })
      .end(JSON.stringify({ ok: false, error: err.message }));
  }
}).listen(process.env.PORT || 3000);
