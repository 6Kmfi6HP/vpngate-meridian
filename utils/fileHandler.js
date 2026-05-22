const fs = require('fs');
const path = require('path');
const crypto = require('crypto');

function parseInteger(value, fallback) {
    const parsed = parseInt(value, 10);
    return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}

function ensureDirectory(dirPath) {
    if (!fs.existsSync(dirPath)) {
        fs.mkdirSync(dirPath, { recursive: true });
    }
}

function stableStringify(value) {
    if (Array.isArray(value)) {
        return `[${value.map(stableStringify).join(',')}]`;
    }

    if (value && typeof value === 'object') {
        return `{${Object.keys(value).sort().map(key => {
            return `${JSON.stringify(key)}:${stableStringify(value[key])}`;
        }).join(',')}}`;
    }

    return JSON.stringify(value);
}

class FileHandler {
    constructor(options = {}) {
        this.outputDir = options.outputDir || process.env.OUTPUT_DIR || 'public';
        this.configDir = path.join(this.outputDir, 'configs');
        this.jsonDir = path.join(this.outputDir, 'json');
        this.statePath = options.statePath || process.env.STATE_PATH || path.join(this.outputDir, 'state', 'servers.json');
        this.activeMissLimit = parseInteger(options.activeMissLimit || process.env.ACTIVE_MISS_LIMIT, 12);
        this.pruneMissLimit = parseInteger(options.pruneMissLimit || process.env.PRUNE_MISS_LIMIT, 48);

        this.ensureDirectories();
    }

    ensureDirectories() {
        [
            this.outputDir,
            this.configDir,
            this.jsonDir,
            path.dirname(this.statePath)
        ].forEach(ensureDirectory);
    }

    hashString(value, algorithm = 'sha256') {
        return crypto.createHash(algorithm).update(String(value || '')).digest('hex');
    }

    slugify(value) {
        const slug = String(value || '')
            .toLowerCase()
            .replace(/[^a-z0-9]+/g, '-')
            .replace(/^-+|-+$/g, '')
            .slice(0, 60);

        return slug || 'server';
    }

    readJsonFile(filePath, fallback) {
        if (!fs.existsSync(filePath)) {
            return fallback;
        }

        try {
            return JSON.parse(fs.readFileSync(filePath, 'utf-8'));
        } catch (error) {
            console.warn(`Could not read ${filePath}: ${error.message}`);
            return fallback;
        }
    }

    writeJsonFile(filePath, value) {
        ensureDirectory(path.dirname(filePath));
        fs.writeFileSync(filePath, `${JSON.stringify(value, null, 4)}\n`, 'utf-8');
    }

    loadPreviousPublishedConfigs() {
        const data = this.readJsonFile(path.join(this.jsonDir, 'data.json'), null);
        if (!data || !data.data || !Array.isArray(data.data.servers)) {
            return {};
        }

        return data.data.servers.reduce((acc, server) => {
            if (server.id && server.openvpn_configdata_base64) {
                acc[server.id] = server.openvpn_configdata_base64;
            }
            return acc;
        }, {});
    }

    hydrateStateServerConfigs(servers) {
        const previousConfigs = this.loadPreviousPublishedConfigs();
        Object.keys(servers).forEach(id => {
            if (!servers[id].openvpn_configdata_base64 && previousConfigs[id]) {
                servers[id].openvpn_configdata_base64 = previousConfigs[id];
            }
        });
    }

    loadState() {
        const state = this.readJsonFile(this.statePath, null);
        if (!state || typeof state !== 'object') {
            return {
                version: 1,
                generatedAt: null,
                servers: {}
            };
        }

        const servers = state.servers || {};
        this.hydrateStateServerConfigs(servers);

        return {
            version: state.version || 1,
            generatedAt: state.generatedAt || null,
            servers
        };
    }

    saveState(state) {
        this.writeJsonFile(this.statePath, state);
    }

    buildServerIdentity(server) {
        const hostname = String(server.hostname || '').trim().toLowerCase();
        if (hostname) {
            return `host:${hostname}`;
        }

        const ip = String(server.ip || '').trim().toLowerCase();
        const country = String(server.countryshort || '').trim().toLowerCase();
        if (ip) {
            return `ip:${ip}|country:${country}`;
        }

        return `config:${this.hashString(server.openvpn_configdata_base64 || '', 'sha1')}`;
    }

    normalizeServer(server) {
        const identity = this.buildServerIdentity(server);
        const id = `${this.slugify(identity.replace(/^[^:]+:/, ''))}-${this.hashString(identity, 'sha1').slice(0, 10)}`;
        const configHash = this.hashString(server.openvpn_configdata_base64 || '');
        const contentHash = this.hashString(stableStringify(server));

        return Object.assign({}, server, {
            id,
            configFilename: `${id}.ovpn`,
            configPath: `configs/${id}.ovpn`,
            configHash,
            contentHash
        });
    }

    summarizeServer(server) {
        return {
            id: server.id,
            hostname: server.hostname,
            ip: server.ip,
            countryshort: server.countryshort,
            countrylong: server.countrylong,
            status: server.status,
            configPath: server.configPath
        };
    }

    buildStateServer(server) {
        const stateServer = Object.assign({}, server);
        if (stateServer.status === 'inactive') {
            delete stateServer.openvpn_configdata_base64;
        }
        return stateServer;
    }

    selectPreferredServer(current, candidate) {
        if (!current) {
            return candidate;
        }

        const currentSpeed = Number(current.speed || 0);
        const candidateSpeed = Number(candidate.speed || 0);
        if (candidateSpeed > currentSpeed) {
            return candidate;
        }

        return current;
    }

    buildCountries(servers, fallbackCountries) {
        const countries = Object.assign({}, fallbackCountries || {});
        servers.forEach(server => {
            const code = String(server.countryshort || '').toLowerCase();
            if (code && server.countrylong) {
                countries[code] = server.countrylong;
            }
        });

        return Object.keys(countries).sort().reduce((acc, key) => {
            acc[key] = countries[key];
            return acc;
        }, {});
    }

    sortServers(servers) {
        return servers.sort((a, b) => {
            const countryCompare = String(a.countryshort || '').localeCompare(String(b.countryshort || ''));
            if (countryCompare !== 0) {
                return countryCompare;
            }

            const hostCompare = String(a.hostname || '').localeCompare(String(b.hostname || ''));
            if (hostCompare !== 0) {
                return hostCompare;
            }

            return String(a.id || '').localeCompare(String(b.id || ''));
        });
    }

    mergeVpnData(currentServers, currentCountries, collectionStats = {}) {
        const now = Date.now();
        const previousState = this.loadState();
        const previousServers = previousState.servers || {};
        const currentById = new Map();
        const publishedById = new Map();

        currentServers.forEach(server => {
            const normalized = this.normalizeServer(server);
            currentById.set(normalized.id, this.selectPreferredServer(currentById.get(normalized.id), normalized));
        });

        const nextServers = {};
        const changes = {
            added: [],
            updated: [],
            recovered: [],
            missing: [],
            inactive: [],
            pruned: [],
            unchangedCount: 0
        };

        Object.keys(previousServers).sort().forEach(id => {
            if (currentById.has(id)) {
                return;
            }

            const previous = previousServers[id];
            const missCount = (previous.missCount || 0) + 1;
            let status = missCount >= this.activeMissLimit ? 'inactive' : 'missing';
            if (status === 'missing' && !previous.openvpn_configdata_base64) {
                status = 'inactive';
            }
            const next = this.buildStateServer(Object.assign({}, previous, {
                missCount,
                status
            }));

            if (previous.status !== 'inactive' && status === 'inactive') {
                changes.inactive.push(this.summarizeServer(next));
            } else if (previous.status !== 'missing' && status === 'missing') {
                changes.missing.push(this.summarizeServer(next));
            }

            if (missCount >= this.pruneMissLimit) {
                changes.pruned.push(this.summarizeServer(next));
                return;
            }

            nextServers[id] = next;
            if (status === 'missing' && next.openvpn_configdata_base64) {
                publishedById.set(id, next);
            }
        });

        Array.from(currentById.entries()).sort(([leftId], [rightId]) => leftId.localeCompare(rightId)).forEach(([id, server]) => {
            const previous = previousServers[id];

            if (!previous) {
                const next = Object.assign({}, server, {
                    firstSeen: now,
                    lastSeen: now,
                    lastChanged: now,
                    seenCount: 1,
                    missCount: 0,
                    status: 'active'
                });

                publishedById.set(id, next);
                nextServers[id] = this.buildStateServer(next);
                changes.added.push(this.summarizeServer(next));
                return;
            }

            const changed = previous.contentHash !== server.contentHash;
            const next = Object.assign({}, server, {
                firstSeen: previous.firstSeen || now,
                lastSeen: now,
                lastChanged: changed ? now : (previous.lastChanged || now),
                seenCount: (previous.seenCount || 0) + 1,
                missCount: 0,
                status: 'active'
            });

            publishedById.set(id, next);
            nextServers[id] = this.buildStateServer(next);

            if (previous.status !== 'active') {
                changes.recovered.push(this.summarizeServer(next));
            } else if (changed) {
                changes.updated.push(this.summarizeServer(next));
            } else {
                changes.unchangedCount += 1;
            }
        });

        const state = {
            version: 1,
            generatedAt: now,
            activeMissLimit: this.activeMissLimit,
            pruneMissLimit: this.pruneMissLimit,
            servers: Object.keys(nextServers).sort().reduce((acc, id) => {
                acc[id] = nextServers[id];
                return acc;
            }, {})
        };

        const stateServers = Object.keys(state.servers).map(id => state.servers[id]);
        const publishedServers = this.sortServers(Array.from(publishedById.values()));
        const activeServers = stateServers.filter(server => server.status === 'active');
        const missingServers = stateServers.filter(server => server.status === 'missing');
        const inactiveServers = stateServers.filter(server => server.status === 'inactive');
        const countries = this.buildCountries(publishedServers, currentCountries);

        const statistics = Object.assign({}, collectionStats, {
            activeServers: activeServers.length,
            publishedServers: publishedServers.length,
            stateServers: stateServers.length,
            missingServers: missingServers.length,
            inactiveServers: inactiveServers.length,
            addedServers: changes.added.length,
            updatedServers: changes.updated.length,
            recoveredServers: changes.recovered.length,
            newlyMissingServers: changes.missing.length,
            newlyInactiveServers: changes.inactive.length,
            prunedServers: changes.pruned.length,
            unchangedServers: changes.unchangedCount,
            totalCountries: Object.keys(countries).length
        });

        return {
            generatedAt: now,
            servers: publishedServers,
            countries,
            state,
            changes,
            statistics
        };
    }

    saveBase64ToFile(base64Data, filename) {
        const buffer = Buffer.from(base64Data || '', 'base64');
        fs.writeFileSync(filename, buffer);
    }

    saveVpnConfigs(servers) {
        ensureDirectory(this.configDir);
        const expectedFiles = new Set(servers.map(server => server.configFilename));

        fs.readdirSync(this.configDir).forEach(file => {
            if (file.endsWith('.ovpn') && !expectedFiles.has(file)) {
                fs.unlinkSync(path.join(this.configDir, file));
            }
        });

        let saved = 0;
        servers.forEach(server => {
            if (!server.openvpn_configdata_base64) {
                return;
            }

            const configPath = path.join(this.configDir, server.configFilename);
            this.saveBase64ToFile(server.openvpn_configdata_base64, configPath);
            saved += 1;
        });

        return saved;
    }

    escapeHtml(value) {
        return String(value == null ? '' : value)
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
            .replace(/"/g, '&quot;')
            .replace(/'/g, '&#39;');
    }

    formatSpeedMbps(value) {
        return (Number(value || 0) / 10000000).toFixed(2);
    }

    generateHomePage(vpnList) {
        const updatedAt = new Date(vpnList.generatedAt).toISOString();
        const statistics = vpnList.statistics || {};
        const countries = Object.keys(vpnList.countries || {}).length;
        const topServers = vpnList.servers
            .slice()
            .sort((a, b) => Number(b.speed || 0) - Number(a.speed || 0))
            .slice(0, 50)
            .map(server => ({
                id: server.id,
                hostname: server.hostname,
                ip: server.ip,
                ping: server.ping,
                speed: server.speed,
                countrylong: server.countrylong,
                countryshort: server.countryshort,
                status: server.status,
                configPath: server.configPath
            }));
        const initialServersJson = JSON.stringify(topServers).replace(/<\//g, '<\\/');
        const initialCountriesJson = JSON.stringify(vpnList.countries || {}).replace(/<\//g, '<\\/');
        const html = `<!doctype html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>VPN Gate Server Index</title>
    <link rel="icon" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 64 64'%3E%3Crect width='64' height='64' rx='12' fill='%2317211d'/%3E%3Cpath d='M18 20h28v8c0 11-5.7 17.7-14 21-8.3-3.3-14-10-14-21z' fill='%230f766e'/%3E%3Cpath d='M26 31h12v5H26z' fill='white'/%3E%3C/svg%3E">
    <style>
        :root {
            color-scheme: light;
            --bg: #f6f7f2;
            --panel: #ffffff;
            --ink: #17211d;
            --muted: #5e6a64;
            --line: #d9ded7;
            --teal: #0f766e;
            --teal-dark: #115e59;
            --amber: #b45309;
            --red: #b91c1c;
            --blue: #2563eb;
            --shadow: 0 18px 45px rgba(31, 41, 35, 0.12);
        }

        * {
            box-sizing: border-box;
        }

        body {
            margin: 0;
            min-width: 320px;
            background:
                linear-gradient(135deg, rgba(15, 118, 110, 0.10), transparent 32%),
                linear-gradient(315deg, rgba(180, 83, 9, 0.10), transparent 34%),
                var(--bg);
            color: var(--ink);
            font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
            line-height: 1.5;
        }

        a {
            color: inherit;
        }

        .shell {
            width: min(1180px, calc(100% - 32px));
            margin: 0 auto;
        }

        .topbar {
            display: flex;
            align-items: center;
            justify-content: space-between;
            gap: 16px;
            padding: 18px 0;
        }

        .brand {
            display: flex;
            align-items: center;
            gap: 12px;
            min-width: 0;
        }

        .brand-mark {
            display: grid;
            width: 40px;
            height: 40px;
            place-items: center;
            border-radius: 8px;
            background: #17211d;
            color: #fff;
            font-weight: 800;
            letter-spacing: 0;
        }

        .brand h1 {
            margin: 0;
            font-size: clamp(1.15rem, 2.5vw, 1.7rem);
            line-height: 1.1;
            letter-spacing: 0;
        }

        .brand p {
            margin: 2px 0 0;
            color: var(--muted);
            font-size: 0.92rem;
        }

        .nav {
            display: flex;
            flex-wrap: wrap;
            justify-content: flex-end;
            gap: 8px;
        }

        .nav a,
        .button {
            display: inline-flex;
            min-height: 38px;
            align-items: center;
            justify-content: center;
            border: 1px solid var(--line);
            border-radius: 8px;
            background: rgba(255, 255, 255, 0.78);
            color: var(--ink);
            padding: 8px 12px;
            font-size: 0.9rem;
            font-weight: 700;
            text-decoration: none;
            white-space: nowrap;
        }

        .button.primary {
            border-color: var(--teal);
            background: var(--teal);
            color: #fff;
        }

        .hero {
            display: grid;
            grid-template-columns: minmax(0, 1.1fr) minmax(320px, 0.9fr);
            gap: 24px;
            align-items: stretch;
            padding: 30px 0 22px;
        }

        .hero-copy {
            display: flex;
            flex-direction: column;
            justify-content: center;
            min-height: 280px;
        }

        .eyebrow {
            margin: 0 0 14px;
            color: var(--teal-dark);
            font-size: 0.82rem;
            font-weight: 800;
            letter-spacing: 0.08em;
            text-transform: uppercase;
        }

        .hero h2 {
            max-width: 760px;
            margin: 0;
            font-size: clamp(2.2rem, 7vw, 5.2rem);
            line-height: 0.98;
            letter-spacing: 0;
        }

        .hero-copy p {
            max-width: 720px;
            margin: 18px 0 0;
            color: var(--muted);
            font-size: clamp(1rem, 2vw, 1.18rem);
        }

        .hero-actions {
            display: flex;
            flex-wrap: wrap;
            gap: 10px;
            margin-top: 24px;
        }

        .signal-panel {
            position: relative;
            overflow: hidden;
            min-height: 320px;
            border: 1px solid var(--line);
            border-radius: 8px;
            background: #10231f;
            box-shadow: var(--shadow);
        }

        .signal-panel::before {
            position: absolute;
            inset: 0;
            content: "";
            background:
                linear-gradient(rgba(255, 255, 255, 0.08) 1px, transparent 1px),
                linear-gradient(90deg, rgba(255, 255, 255, 0.08) 1px, transparent 1px);
            background-size: 34px 34px;
            mask-image: linear-gradient(to bottom, rgba(0, 0, 0, 0.95), rgba(0, 0, 0, 0.35));
        }

        .signal-panel::after {
            position: absolute;
            inset: 16px;
            content: "";
            border: 1px solid rgba(255, 255, 255, 0.18);
            border-radius: 8px;
        }

        .signal-list {
            position: relative;
            z-index: 1;
            display: grid;
            gap: 10px;
            padding: 22px;
        }

        .signal-row {
            display: grid;
            grid-template-columns: 1fr auto;
            gap: 14px;
            align-items: center;
            border: 1px solid rgba(255, 255, 255, 0.15);
            border-radius: 8px;
            background: rgba(255, 255, 255, 0.08);
            padding: 12px;
            color: #fff;
        }

        .signal-row span {
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
        }

        .signal-row strong {
            color: #d9f99d;
            white-space: nowrap;
        }

        .metrics {
            display: grid;
            grid-template-columns: repeat(4, minmax(0, 1fr));
            gap: 12px;
            padding: 18px 0 26px;
        }

        .metric {
            border: 1px solid var(--line);
            border-radius: 8px;
            background: rgba(255, 255, 255, 0.82);
            padding: 18px;
        }

        .metric dt {
            margin: 0 0 8px;
            color: var(--muted);
            font-size: 0.78rem;
            font-weight: 800;
            letter-spacing: 0.08em;
            text-transform: uppercase;
        }

        .metric dd {
            margin: 0;
            font-size: clamp(1.6rem, 4vw, 2.5rem);
            font-weight: 800;
            line-height: 1;
        }

        .workspace {
            display: grid;
            grid-template-columns: 280px minmax(0, 1fr);
            gap: 18px;
            padding-bottom: 36px;
        }

        .filters,
        .table-panel {
            border: 1px solid var(--line);
            border-radius: 8px;
            background: rgba(255, 255, 255, 0.9);
            box-shadow: 0 10px 28px rgba(31, 41, 35, 0.08);
        }

        .filters {
            align-self: start;
            padding: 16px;
        }

        label {
            display: block;
            margin-bottom: 7px;
            color: var(--muted);
            font-size: 0.78rem;
            font-weight: 800;
            letter-spacing: 0.08em;
            text-transform: uppercase;
        }

        input,
        select {
            width: 100%;
            min-height: 42px;
            border: 1px solid var(--line);
            border-radius: 8px;
            background: #fff;
            color: var(--ink);
            padding: 8px 10px;
            font: inherit;
        }

        .field {
            margin-bottom: 14px;
        }

        .status-row {
            display: flex;
            align-items: center;
            justify-content: space-between;
            gap: 12px;
            border-top: 1px solid var(--line);
            margin-top: 14px;
            padding-top: 14px;
            color: var(--muted);
            font-size: 0.9rem;
        }

        .table-panel {
            overflow: hidden;
        }

        .table-heading {
            display: flex;
            align-items: center;
            justify-content: space-between;
            gap: 14px;
            border-bottom: 1px solid var(--line);
            padding: 16px 18px;
        }

        .table-heading h3 {
            margin: 0;
            font-size: 1.05rem;
            letter-spacing: 0;
        }

        .table-heading p {
            margin: 0;
            color: var(--muted);
            font-size: 0.9rem;
        }

        .table-wrap {
            overflow-x: auto;
        }

        table {
            width: 100%;
            min-width: 760px;
            border-collapse: collapse;
        }

        th,
        td {
            border-bottom: 1px solid var(--line);
            padding: 12px 14px;
            text-align: left;
            vertical-align: middle;
        }

        th {
            background: #f1f4ef;
            color: var(--muted);
            font-size: 0.76rem;
            font-weight: 800;
            letter-spacing: 0.08em;
            text-transform: uppercase;
        }

        td {
            font-size: 0.92rem;
        }

        tbody tr:hover {
            background: #fbfcf8;
        }

        .host {
            max-width: 240px;
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
            font-weight: 700;
        }

        .tag {
            display: inline-flex;
            align-items: center;
            min-height: 24px;
            border-radius: 999px;
            background: #e7f6f3;
            color: var(--teal-dark);
            padding: 2px 9px;
            font-size: 0.78rem;
            font-weight: 800;
            text-transform: uppercase;
        }

        .tag.missing {
            background: #fff7ed;
            color: var(--amber);
        }

        .download {
            color: var(--blue);
            font-weight: 800;
            text-decoration: none;
        }

        .empty {
            padding: 36px 18px;
            color: var(--muted);
            text-align: center;
        }

        footer {
            border-top: 1px solid var(--line);
            padding: 22px 0 34px;
            color: var(--muted);
            font-size: 0.9rem;
        }

        @media (max-width: 900px) {
            .topbar,
            .hero,
            .workspace {
                grid-template-columns: 1fr;
            }

            .topbar {
                display: grid;
                align-items: start;
            }

            .nav {
                justify-content: flex-start;
            }

            .metrics {
                grid-template-columns: repeat(2, minmax(0, 1fr));
            }
        }

        @media (max-width: 560px) {
            .shell {
                width: min(100% - 20px, 1180px);
            }

            .metrics {
                grid-template-columns: 1fr;
            }

            .hero h2 {
                font-size: 2.45rem;
            }

            .table-heading {
                display: block;
            }

            .table-heading p {
                margin-top: 6px;
            }
        }
    </style>
</head>
<body>
    <header class="shell topbar">
        <div class="brand">
            <div class="brand-mark" aria-hidden="true">VG</div>
            <div>
                <h1>VPN Gate Server Index</h1>
                <p>Generated from the latest scraper snapshot</p>
            </div>
        </div>
        <nav class="nav" aria-label="Generated output">
            <a href="json/data.json">JSON</a>
            <a href="json/changes.json">Changes</a>
            <a href="mihomo_openvpn.yaml">Mihomo</a>
            <a href="README.md">README</a>
        </nav>
    </header>

    <main>
        <section class="shell hero" aria-labelledby="page-title">
            <div class="hero-copy">
                <p class="eyebrow">Last generated ${this.escapeHtml(updatedAt)}</p>
                <h2 id="page-title">Live VPN Gate output for GitHub Pages.</h2>
                <p>Browse the generated server snapshot, inspect the latest rolling-window status, and download OpenVPN configs directly from the published Pages branch.</p>
                <div class="hero-actions">
                    <a class="button primary" href="json/data.json">Open data.json</a>
                    <a class="button" href="configs/">Browse configs</a>
                </div>
            </div>
            <aside class="signal-panel" aria-label="Fastest server preview">
                <div class="signal-list" id="signal-list">
                    ${topServers.slice(0, 8).map(server => `<div class="signal-row"><span>${this.escapeHtml(server.countryshort || 'N/A')} / ${this.escapeHtml(server.hostname || server.ip || server.id)}</span><strong>${this.formatSpeedMbps(server.speed)} Mbps</strong></div>`).join('\n                    ')}
                </div>
            </aside>
        </section>

        <dl class="shell metrics">
            <div class="metric">
                <dt>Published</dt>
                <dd id="metric-published">${this.escapeHtml(statistics.publishedServers || vpnList.servers.length)}</dd>
            </div>
            <div class="metric">
                <dt>Active</dt>
                <dd id="metric-active">${this.escapeHtml(statistics.activeServers || 0)}</dd>
            </div>
            <div class="metric">
                <dt>Countries</dt>
                <dd id="metric-countries">${this.escapeHtml(statistics.totalCountries || countries)}</dd>
            </div>
            <div class="metric">
                <dt>API Calls</dt>
                <dd id="metric-requests">${this.escapeHtml(statistics.totalRequests || 0)}</dd>
            </div>
        </dl>

        <section class="shell workspace" aria-label="Server browser">
            <form class="filters" id="filters">
                <div class="field">
                    <label for="query">Search</label>
                    <input id="query" type="search" placeholder="Host, IP, or country">
                </div>
                <div class="field">
                    <label for="country">Country</label>
                    <select id="country">
                        <option value="">All countries</option>
                    </select>
                </div>
                <div class="field">
                    <label for="sort">Sort</label>
                    <select id="sort">
                        <option value="speed">Speed, high to low</option>
                        <option value="ping">Ping, low to high</option>
                        <option value="country">Country</option>
                        <option value="host">Hostname</option>
                    </select>
                </div>
                <div class="status-row">
                    <span id="updated-at">${this.escapeHtml(updatedAt)}</span>
                    <button class="button" type="reset">Reset</button>
                </div>
            </form>

            <section class="table-panel">
                <div class="table-heading">
                    <div>
                        <h3>Published Servers</h3>
                        <p id="result-count">${this.escapeHtml(vpnList.servers.length)} servers loaded</p>
                    </div>
                    <a class="button" href="json/data.maxmind.json">MaxMind JSON</a>
                </div>
                <div class="table-wrap">
                    <table>
                        <thead>
                            <tr>
                                <th>Hostname</th>
                                <th>IP</th>
                                <th>Country</th>
                                <th>Ping</th>
                                <th>Speed</th>
                                <th>Status</th>
                                <th>Config</th>
                            </tr>
                        </thead>
                        <tbody id="server-rows"></tbody>
                    </table>
                    <div class="empty" id="empty-state" hidden>No matching servers.</div>
                </div>
            </section>
        </section>
    </main>

    <footer class="shell">
        GitHub Actions publishes this directory to the gh-pages branch. Configure the repository Pages source to serve from gh-pages / root.
    </footer>

    <script>
        (function () {
            var state = {
                servers: ${initialServersJson},
                countries: ${initialCountriesJson},
                generatedAt: '${this.escapeHtml(updatedAt)}',
                statistics: {
                    publishedServers: ${JSON.stringify(statistics.publishedServers || vpnList.servers.length)},
                    activeServers: ${JSON.stringify(statistics.activeServers || 0)},
                    totalCountries: ${JSON.stringify(statistics.totalCountries || countries)},
                    totalRequests: ${JSON.stringify(statistics.totalRequests || 0)}
                }
            };
            var elements = {};

            function number(value) {
                var parsed = Number(value || 0);
                return Number.isFinite(parsed) ? parsed : 0;
            }

            function formatSpeed(value) {
                return (number(value) / 10000000).toFixed(2) + ' Mbps';
            }

            function text(value) {
                return value == null || value === '' ? 'N/A' : String(value);
            }

            function clear(node) {
                while (node.firstChild) {
                    node.removeChild(node.firstChild);
                }
            }

            function addCell(row, value, className) {
                var cell = document.createElement('td');
                if (className) {
                    cell.className = className;
                }
                cell.textContent = text(value);
                row.appendChild(cell);
                return cell;
            }

            function renderCountries() {
                var current = elements.country.value;
                var codes = Object.keys(state.countries || {}).sort();
                clear(elements.country);
                var allOption = document.createElement('option');
                allOption.value = '';
                allOption.textContent = 'All countries';
                elements.country.appendChild(allOption);
                codes.forEach(function (code) {
                    var option = document.createElement('option');
                    option.value = code;
                    option.textContent = state.countries[code] + ' (' + code.toUpperCase() + ')';
                    elements.country.appendChild(option);
                });
                elements.country.value = current;
            }

            function getFilteredServers() {
                var query = elements.query.value.trim().toLowerCase();
                var country = elements.country.value.toLowerCase();
                var sort = elements.sort.value;
                var servers = state.servers.filter(function (server) {
                    var serverCountry = String(server.countryshort || '').toLowerCase();
                    var haystack = [
                        server.hostname,
                        server.ip,
                        server.countryshort,
                        server.countrylong,
                        server.status
                    ].join(' ').toLowerCase();
                    return (!country || serverCountry === country) && (!query || haystack.indexOf(query) !== -1);
                });

                servers.sort(function (left, right) {
                    if (sort === 'ping') {
                        return number(left.ping) - number(right.ping);
                    }
                    if (sort === 'country') {
                        return String(left.countryshort || '').localeCompare(String(right.countryshort || '')) ||
                            String(left.hostname || '').localeCompare(String(right.hostname || ''));
                    }
                    if (sort === 'host') {
                        return String(left.hostname || '').localeCompare(String(right.hostname || ''));
                    }
                    return number(right.speed) - number(left.speed);
                });

                return servers;
            }

            function renderRows() {
                var servers = getFilteredServers();
                var visible = servers.slice(0, 300);
                clear(elements.rows);
                visible.forEach(function (server) {
                    var row = document.createElement('tr');
                    addCell(row, server.hostname || server.id, 'host');
                    addCell(row, server.ip);
                    addCell(row, server.countrylong || server.countryshort);
                    addCell(row, server.ping ? server.ping + ' ms' : 'N/A');
                    addCell(row, formatSpeed(server.speed));
                    var statusCell = document.createElement('td');
                    var tag = document.createElement('span');
                    tag.className = 'tag ' + String(server.status || 'active').toLowerCase();
                    tag.textContent = text(server.status || 'active');
                    statusCell.appendChild(tag);
                    row.appendChild(statusCell);
                    var linkCell = document.createElement('td');
                    var link = document.createElement('a');
                    link.className = 'download';
                    link.href = server.configPath || '#';
                    link.textContent = 'Download';
                    linkCell.appendChild(link);
                    row.appendChild(linkCell);
                    elements.rows.appendChild(row);
                });
                elements.empty.hidden = servers.length !== 0;
                elements.resultCount.textContent = servers.length + ' matching servers' + (servers.length > visible.length ? ', first ' + visible.length + ' shown' : '');
            }

            function renderMetrics() {
                elements.published.textContent = state.statistics.publishedServers || state.servers.length;
                elements.active.textContent = state.statistics.activeServers || state.servers.filter(function (server) {
                    return server.status === 'active';
                }).length;
                elements.countriesMetric.textContent = state.statistics.totalCountries || Object.keys(state.countries || {}).length;
                elements.requests.textContent = state.statistics.totalRequests || 0;
                elements.updatedAt.textContent = state.generatedAt;
            }

            function renderSignals() {
                var fastest = state.servers.slice().sort(function (left, right) {
                    return number(right.speed) - number(left.speed);
                }).slice(0, 8);
                clear(elements.signals);
                fastest.forEach(function (server) {
                    var row = document.createElement('div');
                    row.className = 'signal-row';
                    var name = document.createElement('span');
                    name.textContent = text(server.countryshort) + ' / ' + text(server.hostname || server.ip || server.id);
                    var speed = document.createElement('strong');
                    speed.textContent = formatSpeed(server.speed);
                    row.appendChild(name);
                    row.appendChild(speed);
                    elements.signals.appendChild(row);
                });
            }

            function render() {
                renderCountries();
                renderMetrics();
                renderSignals();
                renderRows();
            }

            function loadData() {
                fetch('json/data.json', { cache: 'no-store' })
                    .then(function (response) {
                        if (!response.ok) {
                            throw new Error('Unable to load data.json');
                        }
                        return response.json();
                    })
                    .then(function (payload) {
                        var data = payload.data || {};
                        state.servers = Array.isArray(data.servers) ? data.servers : state.servers;
                        state.countries = data.countries || state.countries;
                        state.generatedAt = payload.generatedAtIso || state.generatedAt;
                        state.statistics = payload.statistics || state.statistics;
                        render();
                    })
                    .catch(function () {
                        render();
                    });
            }

            document.addEventListener('DOMContentLoaded', function () {
                elements.query = document.getElementById('query');
                elements.country = document.getElementById('country');
                elements.sort = document.getElementById('sort');
                elements.rows = document.getElementById('server-rows');
                elements.empty = document.getElementById('empty-state');
                elements.resultCount = document.getElementById('result-count');
                elements.published = document.getElementById('metric-published');
                elements.active = document.getElementById('metric-active');
                elements.countriesMetric = document.getElementById('metric-countries');
                elements.requests = document.getElementById('metric-requests');
                elements.updatedAt = document.getElementById('updated-at');
                elements.signals = document.getElementById('signal-list');

                elements.query.addEventListener('input', renderRows);
                elements.country.addEventListener('change', renderRows);
                elements.sort.addEventListener('change', renderRows);
                document.getElementById('filters').addEventListener('reset', function () {
                    window.setTimeout(renderRows, 0);
                });

                render();
                loadData();
            });
        }());
    </script>
</body>
</html>
`;

        fs.writeFileSync(path.join(this.outputDir, 'index.html'), html, 'utf-8');
    }

    generateReadme(vpnList) {
        const updatedAt = new Date(vpnList.generatedAt).toISOString();
        let content = '# VPN Gate Data\n\n';
        content += 'This branch contains generated VPN Gate scraper output. The source code lives on the main branch.\n\n';
        content += `Last generated: ${updatedAt}\n\n`;
        content += `Active servers: ${vpnList.servers.length}\n\n`;
        content += 'Machine-readable data: [json/data.json](./json/data.json)\n\n';
        content += '| Hostname | IP Address | Ping | Speed | Country | OpenVPN Config |\n';
        content += '|----------|------------|------|-------|---------|----------------|\n';

        vpnList.servers.forEach(server => {
            const speedInMbps = (Number(server.speed || 0) / 10000000).toFixed(2);
            content += `| ${server.hostname || ''} | ${server.ip || ''} | ${server.ping || ''} | ${speedInMbps} Mbps | ${server.countrylong || ''} | [Download](./${server.configPath}) |\n`;
        });

        fs.writeFileSync(path.join(this.outputDir, 'README.md'), content, 'utf-8');
    }

    saveData(vpnList) {
        const data = {
            generatedAt: vpnList.generatedAt,
            generatedAtIso: new Date(vpnList.generatedAt).toISOString(),
            data: {
                servers: vpnList.servers,
                countries: vpnList.countries
            },
            statistics: vpnList.statistics
        };

        this.writeJsonFile(path.join(this.jsonDir, 'data.json'), data);
    }

    saveChanges(vpnList) {
        const changes = {
            generatedAt: vpnList.generatedAt,
            generatedAtIso: new Date(vpnList.generatedAt).toISOString(),
            summary: {
                added: vpnList.changes.added.length,
                updated: vpnList.changes.updated.length,
                recovered: vpnList.changes.recovered.length,
                missing: vpnList.changes.missing.length,
                inactive: vpnList.changes.inactive.length,
                pruned: vpnList.changes.pruned.length,
                unchanged: vpnList.changes.unchangedCount
            },
            changes: {
                added: vpnList.changes.added,
                updated: vpnList.changes.updated,
                recovered: vpnList.changes.recovered,
                missing: vpnList.changes.missing,
                inactive: vpnList.changes.inactive,
                pruned: vpnList.changes.pruned
            }
        };

        this.writeJsonFile(path.join(this.jsonDir, 'changes.json'), changes);
    }
}

module.exports = FileHandler;
