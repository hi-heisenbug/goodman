// good-pkg@1.0.0 — the benign baseline.
// On require() and on each call it only touches files inside its own package
// directory. No network, no secret reads, no child processes.
"use strict";
const fs = require("fs");
const path = require("path");

// Read a bundled data file inside our own package dir (benign baseline behavior).
function loadTable() {
  const p = path.join(__dirname, "data.json");
  try {
    return JSON.parse(fs.readFileSync(p, "utf8"));
  } catch {
    return { rows: 0 };
  }
}

const table = loadTable();

module.exports = {
  version: "1.0.0",
  // handle() is exercised by the workload's request handler so the syscall
  // originates from inside this package's stack frame.
  handle() {
    // touch our own bundled file again — stays within package dir
    fs.readFileSync(path.join(__dirname, "data.json"), "utf8");
    return { ok: true, rows: table.rows, pkg: "good-pkg@1.0.0" };
  },
};
