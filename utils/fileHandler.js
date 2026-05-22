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
        this.activeMissLimit = parseInteger(options.activeMissLimit || process.env.ACTIVE_MISS_LIMIT, 3);
        this.pruneMissLimit = parseInteger(options.pruneMissLimit || process.env.PRUNE_MISS_LIMIT, 24);

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

    loadState() {
        const state = this.readJsonFile(this.statePath, null);
        if (!state || typeof state !== 'object') {
            return {
                version: 1,
                generatedAt: null,
                servers: {}
            };
        }

        return {
            version: state.version || 1,
            generatedAt: state.generatedAt || null,
            servers: state.servers || {}
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
        delete stateServer.openvpn_configdata_base64;
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
            const status = missCount >= this.activeMissLimit ? 'inactive' : 'missing';
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
        const missingServers = stateServers.filter(server => server.status === 'missing');
        const inactiveServers = stateServers.filter(server => server.status === 'inactive');
        const countries = this.buildCountries(publishedServers, currentCountries);

        const statistics = Object.assign({}, collectionStats, {
            activeServers: publishedServers.length,
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
