#!/usr/bin/env node

const port = Number(process.argv[2]);
if (!Number.isInteger(port) || port <= 0) {
  throw new Error("usage: browser_state.mjs <remote-debugging-port>");
}

const targetsResponse = await fetch(`http://127.0.0.1:${port}/json/list`);
if (!targetsResponse.ok) {
  throw new Error(`Chrome target discovery failed: HTTP ${targetsResponse.status}`);
}
const targets = await targetsResponse.json();
const target =
  targets.find(
    (candidate) =>
      candidate.type === "page" && candidate.url.startsWith("http://127.0.0.1:"),
  ) ?? targets.find((candidate) => candidate.type === "page");
if (!target?.webSocketDebuggerUrl) {
  throw new Error("Chrome did not expose a page target");
}

const state = await new Promise((resolve, reject) => {
  const socket = new WebSocket(target.webSocketDebuggerUrl);
  const timeout = setTimeout(() => {
    socket.close();
    reject(new Error("timed out reading Chrome page state"));
  }, 3000);

  socket.addEventListener("open", () => {
    socket.send(
      JSON.stringify({
        id: 1,
        method: "Runtime.evaluate",
        params: {
          expression:
            "({hash: location.hash, readyState: document.readyState, text: document.body?.innerText ?? ''})",
          returnByValue: true,
        },
      }),
    );
  });

  socket.addEventListener("message", (event) => {
    const message = JSON.parse(String(event.data));
    if (message.id !== 1) return;
    clearTimeout(timeout);
    socket.close();
    if (message.error || message.result?.exceptionDetails) {
      reject(new Error(JSON.stringify(message.error ?? message.result.exceptionDetails)));
      return;
    }
    resolve(message.result.result.value);
  });

  socket.addEventListener("error", () => {
    clearTimeout(timeout);
    reject(new Error("Chrome DevTools websocket failed"));
  });
});

process.stdout.write(`${JSON.stringify(state)}\n`);
