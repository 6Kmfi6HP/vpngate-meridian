const assert = require('assert');
const fs = require('fs');
const os = require('os');
const path = require('path');
const FileHandler = require('../utils/fileHandler');

function createServer(overrides = {}) {
    return Object.assign({
        hostname: 'vpn.example.test',
        ip: '203.0.113.10',
        ping: '20',
        speed: '100000000',
        countrylong: 'Japan',
        countryshort: 'JP',
        openvpn_configdata_base64: Buffer.from('client\nremote 203.0.113.10 1194\n').toString('base64')
    }, overrides);
}

const outputDir = fs.mkdtempSync(path.join(os.tmpdir(), 'vpngate-filehandler-'));
const handler = new FileHandler({
    outputDir,
    statePath: path.join(outputDir, 'state', 'servers.json'),
    activeMissLimit: 2,
    pruneMissLimit: 4
});

let result = handler.mergeVpnData([createServer()], { jp: 'Japan' }, { totalRequests: 1 });
assert.strictEqual(result.changes.added.length, 1);
assert.strictEqual(result.servers.length, 1);
assert.strictEqual(result.statistics.activeServers, 1);
assert.strictEqual(result.statistics.publishedServers, 1);
assert.strictEqual(result.statistics.stateServers, 1);
assert.strictEqual(result.state.servers[result.servers[0].id].openvpn_configdata_base64, undefined);
handler.saveState(result.state);
handler.saveVpnConfigs(result.servers);
assert.ok(fs.existsSync(path.join(outputDir, result.servers[0].configPath)));

result = handler.mergeVpnData([createServer({ speed: '200000000' })], { jp: 'Japan' }, { totalRequests: 1 });
assert.strictEqual(result.changes.updated.length, 1);
assert.strictEqual(result.servers.length, 1);
handler.saveState(result.state);

result = handler.mergeVpnData([], {}, { totalRequests: 1 });
assert.strictEqual(result.servers.length, 0);
assert.strictEqual(result.statistics.activeServers, 0);
assert.strictEqual(result.statistics.publishedServers, 0);
assert.strictEqual(result.statistics.missingServers, 1);
assert.strictEqual(result.statistics.inactiveServers, 0);
assert.strictEqual(result.changes.missing.length, 1);
handler.saveState(result.state);
handler.saveVpnConfigs(result.servers);
assert.strictEqual(fs.readdirSync(path.join(outputDir, 'configs')).filter(file => file.endsWith('.ovpn')).length, 0);

result = handler.mergeVpnData([createServer({ speed: '200000000' })], { jp: 'Japan' }, { totalRequests: 1 });
assert.strictEqual(result.changes.recovered.length, 1);
assert.strictEqual(result.servers.length, 1);
handler.saveState(result.state);

result = handler.mergeVpnData([], {}, { totalRequests: 1 });
assert.strictEqual(result.servers.length, 0);
assert.strictEqual(result.changes.missing.length, 1);
assert.strictEqual(result.statistics.missingServers, 1);
handler.saveState(result.state);

result = handler.mergeVpnData([], {}, { totalRequests: 1 });
assert.strictEqual(result.changes.inactive.length, 1);
assert.strictEqual(result.statistics.missingServers, 0);
assert.strictEqual(result.statistics.inactiveServers, 1);
handler.saveState(result.state);

result = handler.mergeVpnData([], {}, { totalRequests: 1 });
assert.strictEqual(result.statistics.inactiveServers, 1);
handler.saveState(result.state);

result = handler.mergeVpnData([], {}, { totalRequests: 1 });
assert.strictEqual(result.changes.pruned.length, 1);
assert.strictEqual(result.statistics.stateServers, 0);

console.log('fileHandler incremental state tests passed');
