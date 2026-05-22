const { Worker, isMainThread, parentPort, workerData } = require('worker_threads');
const os = require('os');
const VpnScraper = require('./lib/VpnScraper');
const FileHandler = require('./utils/fileHandler');
const cliProgress = require('cli-progress');

function parsePositiveInteger(value, fallback) {
    const parsed = parseInt(value, 10);
    return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}

function distributeRequests(totalRequests, workerCount) {
    const base = Math.floor(totalRequests / workerCount);
    const remainder = totalRequests % workerCount;
    return Array.from({ length: workerCount }, (_, index) => base + (index < remainder ? 1 : 0));
}

if (!isMainThread) {
    const { requestCount, workerId } = workerData;
    const scraper = new VpnScraper();

    async function performRequests() {
        const results = [];
        for (let i = 0; i < requestCount; i++) {
            try {
                const result = await scraper.fetchVpnData();
                results.push(result);
                parentPort.postMessage({ type: 'progress', workerId, current: i + 1, total: requestCount });
            } catch (error) {
                console.error(`Worker ${workerId}: Error in request ${i + 1}: ${error.message}`);
            }
        }
        return results;
    }

    performRequests()
        .then(results => parentPort.postMessage({ type: 'complete', results }))
        .catch(error => {
            console.error(`Worker ${workerId}: Fatal error: ${error.message}`);
            parentPort.postMessage({ type: 'complete', results: [] });
        });
} else {
    async function runMultiThreaded(totalRequests = 1500) {
        const fileHandler = new FileHandler();
        const cpuCount = os.cpus().length;
        const maxWorkers = parsePositiveInteger(process.env.WORKER_COUNT, 8);
        const workerCount = Math.max(1, Math.min(totalRequests, Math.max(1, cpuCount - 1), maxWorkers));
        const requestDistribution = distributeRequests(totalRequests, workerCount);
        const showProgress = !process.env.CI && process.env.NO_PROGRESS !== '1';

        console.log(`Starting VPN data collection with ${workerCount} workers`);
        console.log(`Total requests: ${totalRequests}`);
        console.log(`Output directory: ${fileHandler.outputDir}`);
        console.log(`State file: ${fileHandler.statePath}\n`);

        const multibar = showProgress ? new cliProgress.MultiBar({
            clearOnComplete: false,
            hideCursor: true,
            format: 'Worker {workerId} [{bar}] {percentage}% | {value}/{total} requests | ETA: {eta}s'
        }, cliProgress.Presets.shades_classic) : null;

        const workers = [];
        const allResults = [];
        const progressBars = new Map();
        const workerPromises = [];

        requestDistribution.forEach((requestCount, index) => {
            const workerId = index + 1;
            const worker = new Worker(__filename, {
                workerData: {
                    requestCount,
                    workerId
                }
            });

            if (showProgress) {
                const bar = multibar.create(requestCount, 0, { workerId });
                progressBars.set(workerId, bar);
            }

            const workerPromise = new Promise((resolve, reject) => {
                worker.on('message', message => {
                    if (message.type === 'progress') {
                        const bar = progressBars.get(message.workerId);
                        if (bar) {
                            bar.update(message.current);
                        }
                    } else if (message.type === 'complete') {
                        allResults.push(...message.results);
                        resolve();
                    }
                });

                worker.on('error', reject);
                worker.on('exit', code => {
                    if (code !== 0) {
                        reject(new Error(`Worker stopped with exit code ${code}`));
                    }
                });
            });

            workers.push(worker);
            workerPromises.push(workerPromise);
        });

        try {
            await Promise.all(workerPromises);
            if (multibar) {
                multibar.stop();
            }

            const collectedServers = [];
            const collectedCountries = {};
            allResults.forEach(result => {
                if (!result || !Array.isArray(result.servers)) {
                    return;
                }

                collectedServers.push(...result.servers);
                Object.assign(collectedCountries, result.countries || {});
            });

            if (collectedServers.length === 0) {
                throw new Error('No VPN servers were collected; refusing to publish an empty dataset.');
            }

            const uniqueCurrentHosts = new Set(collectedServers.map(server => server.hostname || server.ip).filter(Boolean));
            const collectionStats = {
                totalRequests,
                successfulRequests: allResults.length,
                collectedServerEntries: collectedServers.length,
                uniqueCurrentServers: uniqueCurrentHosts.size
            };
            const mergedResults = fileHandler.mergeVpnData(collectedServers, collectedCountries, collectionStats);

            console.log('\nSaving generated output...');
            const saveBar = showProgress ? new cliProgress.SingleBar({
                format: 'Saving files [{bar}] {percentage}% | {value}/{total}',
                clearOnComplete: true
            }) : null;

            if (saveBar) {
                saveBar.start(5, 0);
            }

            const savedConfigs = fileHandler.saveVpnConfigs(mergedResults.servers);
            if (saveBar) {
                saveBar.increment();
            }

            fileHandler.generateReadme(mergedResults);
            if (saveBar) {
                saveBar.increment();
            }

            fileHandler.saveData(mergedResults);
            if (saveBar) {
                saveBar.increment();
            }

            fileHandler.saveChanges(mergedResults);
            if (saveBar) {
                saveBar.increment();
            }

            fileHandler.saveState(mergedResults.state);
            if (saveBar) {
                saveBar.increment();
                saveBar.stop();
            }

            console.log('\n=== Final Statistics ===');
            console.log(`Total API calls: ${mergedResults.statistics.totalRequests}`);
            console.log(`Successful API calls: ${mergedResults.statistics.successfulRequests}`);
            console.log(`Collected server entries: ${mergedResults.statistics.collectedServerEntries}`);
            console.log(`Unique current servers: ${mergedResults.statistics.uniqueCurrentServers}`);
            console.log(`Published active servers: ${mergedResults.statistics.publishedServers}`);
            console.log(`Total servers kept in state: ${mergedResults.statistics.stateServers}`);
            console.log(`Missing servers kept in state: ${mergedResults.statistics.missingServers}`);
            console.log(`Inactive servers kept in state: ${mergedResults.statistics.inactiveServers}`);
            console.log(`Added: ${mergedResults.statistics.addedServers}`);
            console.log(`Updated: ${mergedResults.statistics.updatedServers}`);
            console.log(`Recovered: ${mergedResults.statistics.recoveredServers}`);
            console.log(`Newly missing: ${mergedResults.statistics.newlyMissingServers}`);
            console.log(`Newly inactive: ${mergedResults.statistics.newlyInactiveServers}`);
            console.log(`Pruned: ${mergedResults.statistics.prunedServers}`);
            console.log(`Unchanged: ${mergedResults.statistics.unchangedServers}`);
            console.log(`Countries: ${mergedResults.statistics.totalCountries}`);
            console.log(`OpenVPN configs written: ${savedConfigs}`);

            console.log('\nProcess completed successfully!');
            return mergedResults;
        } finally {
            workers.forEach(worker => worker.terminate());
        }
    }

    const totalRequests = parsePositiveInteger(process.env.TOTAL_REQUESTS || process.argv[2], 1500);
    runMultiThreaded(totalRequests).catch(error => {
        console.error(`Error in main thread: ${error.message}`);
        process.exitCode = 1;
    });
}
