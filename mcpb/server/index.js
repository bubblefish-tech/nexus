#!/usr/bin/env node
const fs = require("fs");
const net = require("net");
const os = require("os");

const LOG_PATH = "D:\\Test\\BubbleFish\\v010-dogfood\\bfn-mcpb-diag.log";

function log(line) {
  const stamped = "[" + new Date().toISOString() + "] " + line + "\n";
  try {
    fs.appendFileSync(LOG_PATH, stamped);
  } catch (e) {
    process.stderr.write("LOG_FAIL: " + e.message + " | " + stamped);
  }
}

try { fs.writeFileSync(LOG_PATH, ""); }
catch (e) { process.stderr.write("WIPE_FAIL: " + e.message + "\n"); }

log("=== DIAGNOSTIC BUILD #2 START ===");
log("node version: " + process.version);
log("platform: " + process.platform + " " + process.arch);
log("pid: " + process.pid);
log("cwd: " + process.cwd());
log("execPath: " + process.execPath);
log("argv: " + JSON.stringify(process.argv));
log("HOME env: " + (process.env.HOME || "<UNSET>"));
log("USERPROFILE env: " + (process.env.USERPROFILE || "<UNSET>"));
log("APPDATA env: " + (process.env.APPDATA || "<UNSET>"));
log("BUBBLEFISH_HOME env: " + (process.env.BUBBLEFISH_HOME || "<UNSET>"));
log("os.homedir(): " + os.homedir());
log("os.tmpdir(): " + os.tmpdir());

log("--- full env keys ---");
Object.keys(process.env).sort().forEach((k) => {
  log("  " + k + " = " + process.env[k]);
});
log("--- end env ---");

log("loopback test: connecting to 127.0.0.1:7474 ...");
const sock = new net.Socket();
let resolved = false;
const finish = (result) => {
  if (resolved) return;
  resolved = true;
  log("loopback result: " + result);
  try { sock.destroy(); } catch (_) {}
  setTimeout(() => {
    log("=== DIAGNOSTIC BUILD #2 END ===");
    process.exit(0);
  }, 500);
};

sock.setTimeout(3000);
sock.on("connect", () => finish("CONNECTED"));
sock.on("timeout", () => finish("TIMEOUT after 3s"));
sock.on("error", (e) => finish("ERROR " + e.code + " " + e.message));
try { sock.connect(7474, "127.0.0.1"); }
catch (e) { finish("THROW " + e.message); }

setTimeout(() => {
  log("control test: connecting to 127.0.0.1:1 (should fail fast if loopback works) ...");
  const ctl = new net.Socket();
  let ctlDone = false;
  const ctlFinish = (r) => {
    if (ctlDone) return;
    ctlDone = true;
    log("control result: " + r);
    try { ctl.destroy(); } catch (_) {}
  };
  ctl.setTimeout(3000);
  ctl.on("connect", () => ctlFinish("CONNECTED (unexpected!)"));
  ctl.on("timeout", () => ctlFinish("TIMEOUT after 3s"));
  ctl.on("error", (e) => ctlFinish("ERROR " + e.code + " " + e.message));
  try { ctl.connect(1, "127.0.0.1"); }
  catch (e) { ctlFinish("THROW " + e.message); }
}, 100);