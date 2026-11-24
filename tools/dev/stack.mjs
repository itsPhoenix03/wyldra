// Dev stack launcher — starts the control plane, the Go game server and Vite
// together, with one Ctrl-C stopping all three.
//
// Why a script instead of npm scripts: running three long-lived processes in
// parallel needs either a dependency (concurrently / npm-run-all) or a shell
// idiom that differs between cmd.exe, PowerShell and sh. This repo has kept
// zero server dependencies, and `set X=1&&` in package.json is already
// Windows-only. A ~90-line Node script is the portable answer.
//
//   node tools/dev/stack.mjs            control + game + client
//   node tools/dev/stack.mjs --dev      ...with the tester panels and warp enabled
//   node tools/dev/stack.mjs --no-client   servers only (for test-go.mjs)
//   node tools/dev/stack.mjs --legacy   the pre-migration Node server on :8081
//   node tools/dev/stack.mjs --open-ports   also open the ports to your Tailscale
//                                           network until the stack stops (Windows)
//
// The repo-root .env (gitignored; see .env.example) is loaded for every mode but
// --no-client, so HEARTH_STORE=postgres + DATABASE_URL there switch the game to
// Postgres. Variables already set in the shell win over the file.

import { spawn, spawnSync } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '../..');
const args = new Set(process.argv.slice(2));
const dev = args.has('--dev');
const legacy = args.has('--legacy');
const withClient = !args.has('--no-client');
const openPorts = args.has('--open-ports');

// gameserver/test-go.mjs mints its own tickets (it needs both a dev-claimed and
// an unclaimed one, which no single control-plane policy can produce), so it
// only works against a control plane pinned to its keypair. Read the pair out of
// the test itself rather than copying it: one source of truth, and rotating the
// key there cannot silently desync this launcher.
function testTicketKeys() {
  const src = readFileSync(resolve(ROOT, 'gameserver/test-go.mjs'), 'utf8');
  const grab = (name) => src.match(new RegExp(`const ${name} = '([^']+)'`))?.[1];
  const HEARTH_TICKET_PUBKEY = grab('TEST_PUBKEY');
  const HEARTH_TICKET_PRIVKEY = grab('TEST_PRIVKEY');
  if (!HEARTH_TICKET_PUBKEY || !HEARTH_TICKET_PRIVKEY) {
    console.error('stack: could not read TEST_PUBKEY/TEST_PRIVKEY from gameserver/test-go.mjs');
    process.exit(1);
  }
  return { HEARTH_TICKET_PUBKEY, HEARTH_TICKET_PRIVKEY };
}

// .env: KEY=VALUE lines, # comments, optional `export ` and surrounding quotes.
// Node 18 has no built-in loader (process.loadEnvFile is 20.12+). The wire
// suites (--no-client) skip it: they build and dig in the world they run
// against, and that must never be the real database.
function loadDotEnv() {
  const file = resolve(ROOT, '.env');
  if (!withClient || !existsSync(file)) return {};
  const out = {};
  for (const raw of readFileSync(file, 'utf8').split(/\r?\n/)) {
    const m = raw.match(/^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*?)\s*$/);
    if (!m) continue;
    const v = m[2].match(/^(["'])(.*)\1$/);
    out[m[1]] = v ? v[2] : m[2];
  }
  return out;
}
const dotEnv = loadDotEnv();
for (const [k, v] of Object.entries(dotEnv)) if (process.env[k] === undefined) process.env[k] = v;

// Where the game saves, for the banner. Never print DATABASE_URL whole: it
// carries the password.
function storeLabel() {
  if (legacy) return 'server/save.json';
  if (process.env.HEARTH_NO_PERSIST) return 'none (HEARTH_NO_PERSIST)';
  if ((process.env.HEARTH_STORE || '').trim().toLowerCase() !== 'postgres') return 'json (gameserver/world.<id>.save.json)';
  const at = (process.env.DATABASE_URL || '').split('@').pop().split('?')[0];
  return `postgres ${at || '(DATABASE_URL missing!)'}`;
}

// ANSI colours keep three interleaved streams readable; harmless if unsupported.
const COLOURS = { control: '\x1b[36m', game: '\x1b[32m', client: '\x1b[35m', legacy: '\x1b[33m', ports: '\x1b[34m' };
const RESET = '\x1b[0m';
const tagFor = (name) => `${COLOURS[name] || ''}[${name}]${RESET} `;

function pipeLines(stream, tag) {
  let buf = '';
  stream.on('data', (d) => {
    buf += d.toString();
    const lines = buf.split('\n');
    buf = lines.pop();
    for (const l of lines) if (l.trim()) process.stdout.write(tag + l + '\n');
  });
}

const children = [];
let shuttingDown = false;
let portsProc = null;

function run(name, cmd, cmdArgs, opts = {}) {
  const child = spawn(cmd, cmdArgs, {
    cwd: opts.cwd || ROOT,
    env: { ...process.env, ...(opts.env || {}) },
    stdio: ['ignore', 'pipe', 'pipe'],
    shell: process.platform === 'win32',   // resolves go.exe / npx.cmd from PATH
  });
  const tag = tagFor(name);
  pipeLines(child.stdout, tag);
  pipeLines(child.stderr, tag);
  child.on('exit', (code) => {
    if (shuttingDown) return;
    process.stdout.write(tag + `exited with code ${code}\n`);
    shutdown(code ?? 1);        // one process dying takes the stack down: a half-up
  });                           // stack silently misleads more than it helps
  children.push(child);
  return child;
}

function shutdown(code = 0) {
  if (shuttingDown) return;
  shuttingDown = true;
  // Only the waiter that reports the rule going up; the elevated helper it started
  // watches this process and closes the ports once we have exited.
  if (portsProc && portsProc.exitCode === null) portsProc.kill();
  for (const c of children) {
    if (c.exitCode !== null) continue;
    // On Windows, killing the shell wrapper leaves the real process orphaned.
    if (process.platform === 'win32') spawn('taskkill', ['/pid', c.pid, '/f', '/t'], { stdio: 'ignore' });
    else c.kill('SIGTERM');
  }
  setTimeout(() => process.exit(code), 400);
}

process.on('SIGINT', () => shutdown(0));
process.on('SIGTERM', () => shutdown(0));

if (legacy) {
  // The pre-migration single-server path. The client needs ?legacy=1 to match.
  run('legacy', 'node', ['server/index.js'], { env: dev ? { DEV: '1', HEARTH_ALLOW_WARP: '1' } : {} });
} else {
  // HEARTH_DEV_ALL grants the ticket's dev claim to LOOPBACK clients only, so
  // --dev restores the old `npm run server:dev` ergonomics (F9 kit, F10 panel)
  // without handing world-mutating commands to LAN or remote players.
  run('control', 'node', ['control/index.js'], {
    env: {
      ...(dev ? { HEARTH_DEV_ALL: '1', HEARTH_DEV_TOKS: process.env.HEARTH_DEV_TOKS || '' } : {}),
      // --no-client is the mode the wire suites run against; pin the keypair
      // they sign with so `npm run test:go` needs no extra setup.
      ...(withClient ? {} : testTicketKeys()),
    },
  });
  // No HEARTH_DEV here: the dev panels are gated solely on the `dev` claim the
  // control plane signs into the ticket, per-account. A server-wide env flag
  // would hand world-mutating commands to every connected player.
  // Worlds build lazily by default, which is right for a multi-world process but
  // makes the FIRST join pay ~6s of worldgen. Build eagerly here: a dev stack
  // hosts one world that is certain to be joined, and the wire suites time out
  // waiting for `init` behind a cold build.
  run('game', 'go', ['run', './cmd/hearthd'], {
    cwd: resolve(ROOT, 'gameserver'),
    env: { HEARTH_EAGER_WORLDS: '1', ...(dev ? { HEARTH_ALLOW_WARP: '1' } : {}) },
  });
}

// The client serves baked atlases (tools/atlas/build-atlases.mjs); rebuild them first if
// any art changed. A no-op in well under a second when nothing did.
if (withClient) {
  const atlas = spawnSync(process.execPath, [resolve(ROOT, 'tools/atlas/build-atlases.mjs')], { cwd: ROOT, stdio: 'inherit' });
  if (atlas.status !== 0) { process.stdout.write('[atlas] build failed — not starting the client\n'); shutdown(1); }
  else run('client', 'npx', ['vite', '--host']);
}

// --open-ports: Windows Firewall keeps other machines out by default.
// tools/dev/firewall.ps1 asks for admin once (UAC), opens exactly the ports this
// stack listens on, and leaves an elevated helper waiting on OUR pid to close them.
// It does not rely on shutdown() running, so a crash or a closed terminal closes
// them too. A failure here leaves the stack running for this machine only.
if (openPorts && !shuttingDown) {
  if (process.platform !== 'win32') {
    process.stdout.write(tagFor('ports') + '--open-ports only manages Windows Firewall; nothing to do here\n');
  } else {
    const ports = [
      ...(withClient ? [5173] : []),
      ...(legacy ? [8081] : [Number(process.env.CONTROL_PORT) || 8090, Number(process.env.HEARTH_GAME_PORT) || 8082]),
    ];
    portsProc = spawn('powershell.exe', [
      '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', resolve(ROOT, 'tools/dev/firewall.ps1'),
      'open', '-Ports', ports.join(','), '-WatchPid', String(process.pid),
    ], { stdio: ['ignore', 'pipe', 'pipe'] });
    pipeLines(portsProc.stdout, tagFor('ports'));
    pipeLines(portsProc.stderr, tagFor('ports'));
  }
}

process.stdout.write(
  `\n  stack: ${legacy ? 'legacy :8081' : 'control :8090 + game :8082'}` +
  `${withClient ? ' + client :5173' : ''}\n` +
  `  saves: ${storeLabel()}${Object.keys(dotEnv).length ? '  (.env loaded)' : ''}\n` +
  (dev
    ? `  dev: F9 kit + F10 tester panel + warp, granted to this machine only\n`
    : '') +
  (openPorts && process.platform === 'win32'
    ? `  ports: opening to your Tailscale network (approve the UAC prompt); they close when this stops\n`
    : '') +
  `  Ctrl-C stops everything.\n\n`,
);
