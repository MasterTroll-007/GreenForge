#!/usr/bin/env node
/**
 * Claude Code API Proxy v2.1
 * Bridges GreenForge to local Claude Code subscription.
 * Supports both batch and streaming (SSE) responses.
 * Dynamic model discovery via Anthropic docs + OAuth token refresh.
 *
 * Batch mode: POST /v1/messages → JSON response
 * Stream mode: POST /v1/messages with stream:true → SSE events
 * Models:     GET  /v1/models   → dynamic model list
 */
import { createServer } from 'http';
import { spawn } from 'child_process';
import { existsSync, readFileSync, writeFileSync, readdirSync, statSync, mkdirSync } from 'fs';
import { resolve, join, dirname, sep } from 'path';
import { createInterface } from 'readline';
import { homedir } from 'os';
import { get as httpsGet } from 'https';

const PORT = parseInt(process.env.PROXY_PORT || '18790');
const WORKSPACE = process.env.GF_WORKSPACE || 'C:\\GC';
const MAX_TURNS = parseInt(process.env.MAX_TURNS || '15');
const DOCKER_MOUNT = process.env.DOCKER_MOUNT || '/workspace'; // Docker mount point for WORKSPACE
const OAUTH_CLIENT_ID = '9d1c250a-e61b-44d9-88ed-5944d1962f5e';

// --- Dynamic Model Discovery ---
let modelCache = { models: [], fetchedAt: 0 };
const MODEL_CACHE_TTL = 24 * 60 * 60 * 1000; // 24h

function httpsGetJSON(url, headers = {}, maxRedirects = 5) {
  return new Promise((resolve, reject) => {
    const urlObj = new URL(url);
    const opts = {
      hostname: urlObj.hostname,
      path: urlObj.pathname + urlObj.search,
      headers: { ...headers, 'User-Agent': 'GreenForge-Proxy/2.1' },
    };
    httpsGet(opts, (res) => {
      // Follow redirects
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location && maxRedirects > 0) {
        const newUrl = res.headers.location.startsWith('http')
          ? res.headers.location
          : new URL(res.headers.location, url).href;
        return httpsGetJSON(newUrl, headers, maxRedirects - 1).then(resolve, reject);
      }
      let data = '';
      res.on('data', d => data += d);
      res.on('end', () => {
        try { resolve({ status: res.statusCode, data: JSON.parse(data) }); }
        catch { resolve({ status: res.statusCode, data }); }
      });
    }).on('error', reject);
  });
}

function httpsPost(url, postData, extraHeaders = {}) {
  return new Promise((resolve, reject) => {
    const urlObj = new URL(url);
    const opts = {
      hostname: urlObj.hostname,
      path: urlObj.pathname,
      method: 'POST',
      headers: {
        'Content-Type': 'application/x-www-form-urlencoded',
        'Content-Length': Buffer.byteLength(postData),
        ...extraHeaders,
      },
    };
    import('https').then(https => {
      const req = https.request(opts, (res) => {
        let data = '';
        res.on('data', d => data += d);
        res.on('end', () => {
          try { resolve({ status: res.statusCode, data: JSON.parse(data) }); }
          catch { resolve({ status: res.statusCode, data }); }
        });
      });
      req.on('error', reject);
      req.write(postData);
      req.end();
    });
  });
}

function getOAuthCredentials() {
  try {
    const claudeDir = join(homedir(), '.claude');
    const currentAccount = readFileSync(join(claudeDir, 'current-account.txt'), 'utf8').trim();
    if (!currentAccount) return null;
    const accountData = JSON.parse(readFileSync(join(claudeDir, 'accounts', currentAccount + '.json'), 'utf8'));
    return accountData.claudeAiOauth || null;
  } catch { return null; }
}

async function refreshOAuthToken(refreshToken) {
  try {
    const postData = `grant_type=refresh_token&refresh_token=${encodeURIComponent(refreshToken)}&client_id=${OAUTH_CLIENT_ID}`;
    const resp = await httpsPost('https://console.anthropic.com/v1/oauth/token', postData);
    if (resp.status === 200 && resp.data?.access_token) {
      return resp.data;
    }
    return null;
  } catch { return null; }
}

async function fetchModelsFromAPI(accessToken) {
  try {
    const resp = await httpsGetJSON(
      'https://api.anthropic.com/v1/models?limit=100',
      { 'Authorization': `Bearer ${accessToken}`, 'anthropic-version': '2023-06-01' }
    );
    if (resp.status === 200 && resp.data?.data) {
      return resp.data.data.map(m => m.id).filter(id => /^claude-(opus|sonnet|haiku)-/.test(id));
    }
  } catch {}
  return [];
}

async function fetchModelsFromDocs() {
  try {
    const resp = await httpsGetJSON('https://docs.anthropic.com/en/docs/about-claude/models');
    const html = typeof resp.data === 'string' ? resp.data : JSON.stringify(resp.data);
    const modelPattern = /claude-(opus|sonnet|haiku)-[0-9][a-z0-9.-]*/g;
    const matches = new Set();
    let match;
    while ((match = modelPattern.exec(html)) !== null) {
      const id = match[0];
      // Filter noise: must be a real model ID pattern
      if (/^claude-(opus|sonnet|haiku)-\d/.test(id) && !id.includes('v1')) {
        matches.add(id);
      }
    }
    return [...matches].sort();
  } catch { return []; }
}

async function getModels() {
  // Return cache if fresh
  if (modelCache.models.length > 0 && Date.now() - modelCache.fetchedAt < MODEL_CACHE_TTL) {
    return modelCache.models;
  }

  console.log(`[${ts()}] Fetching models...`);

  // Strategy 1: Try OAuth token refresh → API call
  const oauth = getOAuthCredentials();
  if (oauth?.refreshToken) {
    try {
      const tokenResp = await refreshOAuthToken(oauth.refreshToken);
      if (tokenResp?.access_token) {
        const apiModels = await fetchModelsFromAPI(tokenResp.access_token);
        if (apiModels.length > 0) {
          console.log(`[${ts()}] Got ${apiModels.length} models from API`);
          modelCache = { models: apiModels, fetchedAt: Date.now() };
          return apiModels;
        }
      }
    } catch (e) {
      console.log(`[${ts()}] API model fetch failed: ${e.message}`);
    }
  }

  // Strategy 2: Scrape from docs page
  const docModels = await fetchModelsFromDocs();
  if (docModels.length > 0) {
    console.log(`[${ts()}] Got ${docModels.length} models from docs`);
    modelCache = { models: docModels, fetchedAt: Date.now() };
    return docModels;
  }

  // Strategy 3: Fallback - known models (updated periodically)
  const fallback = [
    'claude-haiku-4-5', 'claude-haiku-4-5-20251001',
    'claude-sonnet-4-5', 'claude-sonnet-4-5-20250929',
    'claude-sonnet-4-6',
    'claude-opus-4-5', 'claude-opus-4-5-20251101',
    'claude-opus-4-6',
  ];
  console.log(`[${ts()}] Using fallback model list`);
  modelCache = { models: fallback, fetchedAt: Date.now() };
  return fallback;
}

async function handleModels(res) {
  const models = await getModels();

  // Format like Anthropic API response
  const data = models.map(id => {
    const parts = id.match(/^claude-(opus|sonnet|haiku)-(.+)$/);
    const family = parts ? parts[1] : 'unknown';
    const isAlias = !/\d{8}/.test(id); // No date = alias (latest)
    return {
      id,
      type: 'model',
      display_name: `Claude ${family.charAt(0).toUpperCase() + family.slice(1)} ${parts ? parts[2] : ''}`.trim(),
      family,
      is_alias: isAlias,
    };
  });

  res.writeHead(200, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify({
    data,
    has_more: false,
    object: 'list',
  }));
}

function resolveCwd(input) {
  if (!input) return undefined;
  // Docker paths: /workspace/mhub → strip prefix
  if (input.startsWith('/workspace/')) {
    input = input.replace('/workspace/', '');
  }
  // Try as absolute path first
  if (existsSync(input)) return resolve(input);
  // Try relative to workspace
  const resolved = resolve(WORKSPACE, input);
  if (existsSync(resolved)) return resolved;
  return undefined;
}

function extractPrompt(request) {
  let prompt = '';
  let systemPrompt = request.system || '';
  const extractText = (content) => {
    if (typeof content === 'string') return content;
    if (Array.isArray(content)) return content.filter(c => c.type === 'text').map(c => c.text).join('\n');
    return String(content || '');
  };
  for (const msg of (request.messages || [])) {
    if (msg.role === 'system') systemPrompt = extractText(msg.content);
    if (msg.role === 'user') prompt = extractText(msg.content);
  }
  return { prompt, systemPrompt };
}

function spawnClaude(args, cwd) {
  const env = { ...process.env };
  delete env.CLAUDECODE;
  delete env.CLAUDE_CODE;
  const spawnOpts = { stdio: ['pipe', 'pipe', 'pipe'], env };
  if (cwd) spawnOpts.cwd = cwd;
  return spawn('claude', args, spawnOpts);
}

// --- Streaming handler (SSE) ---
async function handleStreaming(res, request) {
  const { prompt, systemPrompt } = extractPrompt(request);
  const model = request.model || 'claude-sonnet-4-6';
  const cwd = resolveCwd(request.cwd);

  if (!prompt) {
    res.writeHead(400, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ error: 'No user message found' }));
    return;
  }

  console.log(`[${ts()}] STREAM: model=${model} cwd=${cwd || '(none)'} prompt="${prompt.substring(0, 80)}..."`);

  res.writeHead(200, {
    'Content-Type': 'text/event-stream',
    'Cache-Control': 'no-cache',
    'Connection': 'keep-alive',
  });

  // Use stream-json for line-by-line events (requires --verbose)
  const args = ['--print', '--verbose', '--output-format', 'stream-json', '--model', model, '--max-turns', String(MAX_TURNS)];
  if (systemPrompt) args.push('--system-prompt', systemPrompt);

  const proc = spawnClaude(args, cwd);
  proc.stdin.write(prompt);
  proc.stdin.end();

  // Send SSE helper
  const sendSSE = (event, data) => {
    res.write(`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`);
  };

  // Send initial ping
  sendSSE('ping', { status: 'processing' });

  let fullText = '';
  let lastToolName = '';

  // Parse stream-json output line by line
  const rl = createInterface({ input: proc.stdout });

  rl.on('line', (line) => {
    line = line.trim();
    if (!line) return;

    try {
      const event = JSON.parse(line);

      // stream-json emits various event types
      if (event.type === 'assistant' && event.message) {
        // Assistant text message
        const msg = event.message;
        if (msg.type === 'text' || typeof msg.content === 'string') {
          const text = msg.content || msg.text || '';
          if (text) {
            fullText += text;
            sendSSE('text', { text });
          }
        }
        // Content blocks
        if (Array.isArray(msg.content)) {
          for (const block of msg.content) {
            if (block.type === 'text' && block.text) {
              fullText += block.text;
              sendSSE('text', { text: block.text });
            }
            if (block.type === 'tool_use') {
              lastToolName = block.name || '';
              sendSSE('tool_use', { name: block.name, id: block.id });
            }
          }
        }
      } else if (event.type === 'tool_use' || event.type === 'tool_call') {
        lastToolName = event.name || event.tool || '';
        sendSSE('tool_use', { name: lastToolName, input: event.input });
      } else if (event.type === 'tool_result') {
        sendSSE('tool_result', { name: lastToolName, content: String(event.content || '').substring(0, 200) });
      } else if (event.type === 'result') {
        // Final result
        const resultText = event.result || '';
        if (resultText && !fullText.includes(resultText)) {
          fullText = resultText;
          sendSSE('text', { text: resultText });
        }
        if (event.subtype === 'error_max_turns') {
          sendSSE('error', { message: 'Agent used all turns. Partial results above.' });
        }
      } else if (event.type === 'content_block_delta') {
        if (event.delta?.type === 'text_delta' && event.delta.text) {
          fullText += event.delta.text;
          sendSSE('text', { text: event.delta.text });
        }
      }
    } catch {
      // Not JSON - might be raw text output
      if (line && !line.startsWith('{')) {
        fullText += line + '\n';
        sendSSE('text', { text: line + '\n' });
      }
    }
  });

  let stderr = '';
  proc.stderr.on('data', d => { stderr += d; });

  proc.on('close', (code) => {
    // If we got no text at all, send error
    if (!fullText.trim()) {
      if (stderr) {
        sendSSE('error', { message: `Claude error: ${stderr.substring(0, 300)}` });
      } else {
        sendSSE('error', { message: 'No response from Claude.' });
      }
    }
    sendSSE('done', { text: fullText, model });
    res.end();
    console.log(`[${ts()}] STREAM done: ${fullText.substring(0, 100)}...`);
  });

  proc.on('error', (err) => {
    sendSSE('error', { message: err.message });
    res.end();
  });

  // Timeout
  const timeout = setTimeout(() => {
    proc.kill();
    sendSSE('error', { message: 'Timeout (5 min)' });
    res.end();
  }, 5 * 60 * 1000);

  proc.on('close', () => clearTimeout(timeout));

  // Handle client disconnect
  res.on('close', () => {
    proc.kill();
    clearTimeout(timeout);
  });
}

// --- Batch handler (original) ---
async function handleBatch(res, request) {
  const { prompt, systemPrompt } = extractPrompt(request);
  const model = request.model || 'claude-sonnet-4-6';
  const cwd = resolveCwd(request.cwd);

  if (!prompt) {
    res.writeHead(400, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ error: 'No user message found' }));
    return;
  }

  console.log(`[${ts()}] BATCH: model=${model} cwd=${cwd || '(none)'} prompt="${prompt.substring(0, 80)}..."`);

  const args = ['--print', '--output-format', 'json', '--model', model, '--max-turns', String(MAX_TURNS)];
  if (systemPrompt) args.push('--system-prompt', systemPrompt);

  const responseText = await new Promise((resolve, reject) => {
    const proc = spawnClaude(args, cwd);
    let stdout = '';
    let stderr = '';
    proc.stdout.on('data', d => stdout += d);
    proc.stderr.on('data', d => stderr += d);
    proc.stdin.write(prompt);
    proc.stdin.end();
    proc.on('close', (code) => {
      if (code !== 0 && !stdout) {
        reject(new Error(`claude exited ${code}: ${stderr}`));
        return;
      }
      let text = '';
      try {
        const result = JSON.parse(stdout);
        if (typeof result.result === 'string') {
          text = result.result;
        } else if (typeof result.content === 'string') {
          text = result.content;
        } else if (result.type === 'result' && result.subtype) {
          text = `[Agent used all turns: ${result.subtype}]`;
        } else if (Array.isArray(result.content)) {
          text = result.content.filter(c => c.type === 'text').map(c => c.text).join('\n');
        } else {
          text = stdout.trim();
        }
      } catch {
        text = stdout.trim();
      }
      if (text.startsWith('{"type":"result"')) {
        text = 'Sorry, the AI response was not available. Please try again.';
      }
      resolve(text);
    });
    proc.on('error', reject);
    setTimeout(() => { proc.kill(); reject(new Error('timeout')); }, 5 * 60 * 1000);
  });

  console.log(`[${ts()}] BATCH done: ${String(responseText).substring(0, 100)}...`);

  const response = {
    id: `msg_proxy_${Date.now()}`,
    type: 'message',
    role: 'assistant',
    model: model,
    content: [{ type: 'text', text: String(responseText) }],
    stop_reason: 'end_turn',
    usage: { input_tokens: 0, output_tokens: 0 }
  };

  res.writeHead(200, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify(response));
}

// --- Persistent Settings (survives proxy/Docker restarts) ---
const SETTINGS_DIR = join(homedir(), '.greenforge');
const SETTINGS_FILE = join(SETTINGS_DIR, 'proxy-settings.json');

function loadSettings() {
  try {
    if (existsSync(SETTINGS_FILE)) return JSON.parse(readFileSync(SETTINGS_FILE, 'utf8'));
  } catch {}
  return {};
}
function saveSettings(data) {
  try {
    mkdirSync(SETTINGS_DIR, { recursive: true });
    writeFileSync(SETTINGS_FILE, JSON.stringify(data, null, 2));
  } catch (e) { console.error('Failed to save settings:', e.message); }
}

// Load persisted workspace paths (Windows paths)
let proxySettings = loadSettings();
// workspacePaths: array of Windows paths like ['C:/GC', 'C:/PROJECTS']
if (!proxySettings.workspacePaths) {
  proxySettings.workspacePaths = [resolve(WORKSPACE).replace(/\\/g, '/')];
  saveSettings(proxySettings);
}

// Map Windows path → Docker mount point
// C:/GC → /mnt/GC, C:/PROJECTS → /mnt/PROJECTS, etc.
function winPathToMount(winPath) {
  // Normalize
  const norm = winPath.replace(/\\/g, '/').replace(/\/$/, '');
  const name = norm.split('/').pop(); // last segment: GC, PROJECTS, etc.
  return '/mnt/' + name;
}

function getMountMappings() {
  return proxySettings.workspacePaths.map(wp => ({
    win_path: wp,
    docker_mount: winPathToMount(wp),
  }));
}

function winToDockerPath(winPath) {
  const norm = winPath.replace(/\\/g, '/');
  for (const wp of proxySettings.workspacePaths) {
    if (norm.startsWith(wp)) {
      return winPathToMount(wp) + norm.slice(wp.length);
    }
  }
  return winPath; // passthrough
}

// --- Projects handler (scan Windows dirs for git repos) ---
function handleProjects(res, url) {
  const customPath = url.searchParams.get('path');
  const pathsToScan = customPath ? [resolve(customPath).replace(/\\/g, '/')] : proxySettings.workspacePaths;

  const projects = [];
  const seen = new Set();

  for (const wsPath of pathsToScan) {
    let entries;
    try { entries = readdirSync(wsPath, { withFileTypes: true }); } catch { continue; }
    for (const entry of entries) {
      if (!entry.isDirectory() || entry.name.startsWith('.') || HIDDEN_DIRS.has(entry.name)) continue;
      if (seen.has(entry.name)) continue;
      seen.add(entry.name);
      const fullPath = join(wsPath, entry.name).replace(/\\/g, '/');
      let isGit = false;
      try { isGit = statSync(join(fullPath, '.git')).isDirectory(); } catch {}
      projects.push({
        name: entry.name,
        path: fullPath,
        docker_path: winToDockerPath(fullPath),
        git: isGit,
      });
    }
  }

  projects.sort((a, b) => a.name.localeCompare(b.name));

  res.writeHead(200, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify({
    projects,
    workspaces: proxySettings.workspacePaths,
    mounts: getMountMappings(),
  }));
}

// --- Workspace settings handler ---
function handleWorkspace(req, res, url) {
  if (req.method === 'GET') {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({
      paths: proxySettings.workspacePaths,
      mounts: getMountMappings(),
    }));
    return;
  }
  if (req.method === 'PUT') {
    let body = '';
    req.on('data', d => body += d);
    req.on('end', () => {
      try {
        const data = JSON.parse(body);
        if (data.paths && Array.isArray(data.paths) && data.paths.length > 0) {
          proxySettings.workspacePaths = data.paths;
          saveSettings(proxySettings);
          console.log(`[${ts()}] Workspace paths updated:`, data.paths);
        }
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ status: 'ok', paths: proxySettings.workspacePaths, mounts: getMountMappings() }));
      } catch (e) {
        res.writeHead(400, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ error: e.message }));
      }
    });
    return;
  }
  res.writeHead(405); res.end('Method not allowed');
}

// --- Browse handler (Windows host filesystem) ---
const HIDDEN_DIRS = new Set(['node_modules', '$RECYCLE.BIN', '$Recycle.Bin', '$WinREAgent', 'System Volume Information', 'Recovery', 'PerfLogs']);

function handleBrowse(res, url) {
  const rawPath = url.searchParams.get('path');
  const common = { platform: 'win32', workspace: resolve(WORKSPACE).replace(/\\/g, '/'), docker_mount: DOCKER_MOUNT };

  // Empty path = show drives
  if (!rawPath) {
    const drives = [];
    for (const letter of 'CDEFGHIJKLMNOPQRSTUVWXYZ') {
      try {
        readdirSync(letter + ':\\');
        drives.push(letter + ':/');
      } catch {}
    }
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ path: '', parent: '', entries: [], drives, ...common }));
    return;
  }

  const requestedPath = resolve(rawPath);

  try {
    const entries = readdirSync(requestedPath, { withFileTypes: true });
    const dirs = [];
    for (const entry of entries) {
      if (!entry.isDirectory()) continue;
      if (entry.name.startsWith('.') || entry.name.startsWith('$') || HIDDEN_DIRS.has(entry.name)) continue;
      const fullPath = join(requestedPath, entry.name);
      let isGit = false;
      try { isGit = statSync(join(fullPath, '.git')).isDirectory(); } catch {}
      dirs.push({ name: entry.name, path: fullPath.replace(/\\/g, '/'), is_dir: true, is_git: isGit });
    }
    dirs.sort((a, b) => a.name.localeCompare(b.name));

    const parent = dirname(requestedPath).replace(/\\/g, '/');

    // Detect if we're at a drive root (show drives button)
    const atDriveRoot = requestedPath.replace(/\\/g, '/').match(/^[A-Z]:\/$/i);
    let drives = undefined;
    if (atDriveRoot) {
      drives = [];
      for (const letter of 'CDEFGHIJKLMNOPQRSTUVWXYZ') {
        try { readdirSync(letter + ':\\'); drives.push(letter + ':/'); } catch {}
      }
    }

    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ path: requestedPath.replace(/\\/g, '/'), parent, entries: dirs, drives, ...common }));
  } catch (err) {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ path: requestedPath.replace(/\\/g, '/'), parent: dirname(requestedPath).replace(/\\/g, '/'), entries: [], error: err.message, ...common }));
  }
}

function ts() { return new Date().toLocaleTimeString(); }

// --- Server ---
const server = createServer(async (req, res) => {
  res.setHeader('Access-Control-Allow-Origin', '*');
  res.setHeader('Access-Control-Allow-Methods', 'POST, GET, OPTIONS');
  res.setHeader('Access-Control-Allow-Headers', '*');

  const url = new URL(req.url, `http://${req.headers.host}`);

  if (req.method === 'OPTIONS') { res.writeHead(200); res.end(); return; }

  // GET /v1/models - dynamic model list
  if (req.method === 'GET' && url.pathname === '/v1/models') {
    try {
      await handleModels(res);
    } catch (err) {
      res.writeHead(500, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ error: err.message }));
    }
    return;
  }

  // GET /v1/browse - Windows host filesystem browser
  if (req.method === 'GET' && url.pathname === '/v1/browse') {
    handleBrowse(res, url);
    return;
  }

  // GET /v1/projects - scan workspace dirs for git repos
  if (req.method === 'GET' && url.pathname === '/v1/projects') {
    handleProjects(res, url);
    return;
  }

  // GET/PUT /v1/workspace - workspace path management
  if (url.pathname === '/v1/workspace') {
    handleWorkspace(req, res, url);
    return;
  }

  if (req.method === 'GET') {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ status: 'ok', proxy: 'claude-code', version: '2.1', max_turns: MAX_TURNS }));
    return;
  }
  if (req.method !== 'POST') { res.writeHead(405); res.end('Method not allowed'); return; }

  let body = '';
  for await (const chunk of req) body += chunk;

  try {
    const request = JSON.parse(body);
    if (request.stream) {
      await handleStreaming(res, request);
    } else {
      await handleBatch(res, request);
    }
  } catch (err) {
    console.error('Error:', err.message);
    res.writeHead(500, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ type: 'error', error: { type: 'proxy_error', message: err.message } }));
  }
});

server.listen(PORT, '0.0.0.0', () => {
  console.log(`\n  Claude Code API Proxy v2.1`);
  console.log(`  Listening on http://0.0.0.0:${PORT}`);
  console.log(`  Workspace: ${WORKSPACE}`);
  console.log(`  Max turns: ${MAX_TURNS}`);
  console.log(`  Modes: batch (JSON) + streaming (SSE)`);
  console.log(`  Models: GET /v1/models (dynamic, 24h cache)`);
  console.log('');
  // Pre-warm model cache
  getModels().catch(() => {});
});
