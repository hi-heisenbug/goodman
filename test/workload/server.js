// The "victim service": a plain-Node HTTP server that require()s good-pkg and
// invokes it on every request, so the package's syscalls originate from inside
// its own JIT stack frames and Goodman can attribute them.
//
// Start with:  node --perf-basic-prof-only-functions --interpreted-frames-native-stack server.js
"use strict";
const http = require("http");
const good = require("good-pkg");

const PORT = process.env.PORT || 8080;

const server = http.createServer((req, res) => {
  if (req.url === "/healthz") {
    res.writeHead(200, { "content-type": "application/json" });
    res.end(JSON.stringify({ status: "ok" }));
    return;
  }
  // Delegate into the dependency — this is the frame Goodman attributes.
  const out = good.handle();
  res.writeHead(200, { "content-type": "application/json" });
  res.end(JSON.stringify(out));
});

server.listen(PORT, () => {
  console.log(`workload listening on :${PORT} using ${good.pkg || good.version}`);
});

// Graceful shutdown so restarts between fixture versions are clean.
for (const sig of ["SIGINT", "SIGTERM"]) {
  process.on(sig, () => {
    server.close(() => process.exit(0));
  });
}
