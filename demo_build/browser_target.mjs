#!/usr/bin/env node

const port = Number(process.argv[2]);
const targetText = process.argv[3];
const controlText = process.argv[4] ?? "";
if (!Number.isInteger(port) || port <= 0 || !targetText) {
  throw new Error(
    "usage: browser_target.mjs <remote-debugging-port> <target-text> [control-text]",
  );
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

const expression = `(${async (wantedText, wantedControl) => {
  const cards = [...document.querySelectorAll("article.alert-card")];
  const card = cards.find((candidate) => candidate.innerText.includes(wantedText));
  if (!card) return { ok: false, reason: `alert card not found: ${wantedText}` };
  const controls = [...card.querySelectorAll("button, a")];
  const element = wantedControl
    ? controls.find((candidate) => candidate.textContent?.includes(wantedControl))
    : card;
  if (!element) {
    return { ok: false, reason: `control not found: ${wantedControl}` };
  }
  element.scrollIntoView({ block: "center", inline: "nearest" });
  await new Promise((resolve) => requestAnimationFrame(() => resolve(undefined)));
  const rect = element.getBoundingClientRect();
  return {
    ok: rect.width > 0 && rect.height > 0,
    x: Math.round(rect.left + rect.width / 2),
    y: Math.round(rect.top + rect.height / 2),
    text: element.textContent?.trim() ?? "",
  };
}})(${JSON.stringify(targetText)}, ${JSON.stringify(controlText)})`;

const result = await new Promise((resolve, reject) => {
  const socket = new WebSocket(target.webSocketDebuggerUrl);
  const timeout = setTimeout(() => {
    socket.close();
    reject(new Error("timed out resolving Chrome target"));
  }, 3000);

  socket.addEventListener("open", () => {
    socket.send(
      JSON.stringify({
        id: 1,
        method: "Runtime.evaluate",
        params: { expression, returnByValue: true, awaitPromise: true },
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

if (!result?.ok) {
  throw new Error(result?.reason ?? "Chrome target has no visible bounds");
}
process.stdout.write(`${JSON.stringify(result)}\n`);
