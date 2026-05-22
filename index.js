const { Worker, isMainThread, parentPort, workerData } = require('worker_threads');
const os = require('os');
const VpnScraper = require('./lib/VpnScraper');
const FileHandler = require('./utils/fileHandler');
const cliProgress = require('cli-progress');

// Worker thread logic
if (!isMainThread) {
    const { requestCount, workerId } = workerData;
    const scraper = new VpnScraper();
    
    async function performRequests() {
        const results = [];
        for (let i = 0; i < requestCount; i++) {
            try {
                const result = await scraper.fetchVpnData();
                results.push(result);
                // Report progress to main thread
                parentPort.postMessage({ type: 'progress', workerId, current: i + 1, total: requestCount });
                
                // Wait between requests to avoid rate limiting
                // if (i < requestCount - 1) {
                //     await new Promise(resolve => setTimeout(resolve, 1000));
                // }
            } catch (error) {
                console.error(`Worker ${workerId}: Error in request ${i + 1}:`, error.message);
            }
        }
        return results;
    }

    performRequests()
        .then(results => parentPort.postMessage({ type: 'complete', results }))
        .catch(error => {
            console.error(`Worker ${workerId}: Fatal error:`, error);
            parentPort.postMessage({ type: 'complete', results: [] });
        });
}

// Main thread logic
else {
    async function runMultiThreaded(totalRequests = 1500) {
        const fileHandler = new FileHandler();
        const cpuCount = os.cpus().length;
        const workerCount = Math.min(cpuCount - 1, 8);
        const requestsPerWorker = Math.ceil(totalRequests / workerCount);
        
        console.log(`Starting VPN data collection with ${workerCount} workers`);
        console.log(`Each worker will make ${requestsPerWorker} requests\n`);
        
        // Create progress bars
        const multibar = new cliProgress.MultiBar({
            clearOnComplete: false,
            hideCursor: true,
            format: 'Worker {workerId} [{bar}] {percentage}% | {value}/{total} requests | ETA: {eta}s'
        }, cliProgress.Presets.shades_classic);

        const workers = [];
        const allResults = [];
        const progressBars = new Map();
        const workerPromises = [];
        
        for (let i = 0; i < workerCount; i++) {
            const workerId = i + 1;
            const worker = new Worker(__filename, {
                workerData: {
                    requestCount: requestsPerWorker,
                    workerId
                }
            });
            
            // Create progress bar for this worker
            const bar = multibar.create(requestsPerWorker, 0, { workerId });
            progressBars.set(workerId, bar);
            
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
        }
        
        try {
            await Promise.all(workerPromises);
            multibar.stop();
            
            console.log('\nProcessing results...');
            
            // Process and deduplicate results
            const uniqueServers = new Map();
            const allCountries = {};
            let totalServersFound = allResults.length;
            
            const dedupeBar = new cliProgress.SingleBar({
                format: 'Deduplicating servers [{bar}] {percentage}% | {value}/{total}',
                clearOnComplete: true
            });
            
            dedupeBar.start(totalServersFound, 0);
            let processed = 0;
            
            allResults.forEach(result => {
                result.servers.forEach(server => {
                    uniqueServers.set(server.hostname, server);
                    processed++;
                    dedupeBar.update(processed);
                });
                Object.assign(allCountries, result.countries);
            });
            
            dedupeBar.stop();
            
            const finalResults = {
                servers: Array.from(uniqueServers.values()),
                countries: allCountries
            };
            
            // Calculate statistics
            const statistics = {
                totalRequests,
                totalServersFound,
                uniqueServers: finalResults.servers.length,
                duplicateEntries: totalServersFound - finalResults.servers.length,
                totalCountries: Object.keys(finalResults.countries).length
            };
            
            console.log('\nSaving results...');
            const saveBar = new cliProgress.SingleBar({
                format: 'Saving files [{bar}] {percentage}% | {value}/{total}',
                clearOnComplete: true
            });
            
            saveBar.start(3, 0);
            
            // Save results
            fileHandler.saveVpnConfigs(finalResults.servers);
            saveBar.increment();
            
            fileHandler.generateReadme(finalResults);
            saveBar.increment();
            
            fileHandler.saveData(finalResults, statistics);
            saveBar.increment();
            
            saveBar.stop();
            
            console.log('\n=== Final Statistics ===');
            console.log(`Total API calls: ${statistics.totalRequests}`);
            console.log(`Total servers found: ${statistics.totalServersFound}`);
            console.log(`Unique servers: ${statistics.uniqueServers}`);
            console.log(`Duplicate entries: ${statistics.duplicateEntries}`);
            console.log(`Total countries: ${statistics.totalCountries}`);
            
            console.log('\nProcess completed successfully!');
        } catch (error) {
            console.error('Error in main thread:', error);
        } finally {
            // Terminate all workers
            workers.forEach(worker => worker.terminate());
        }
    }

    // Start the process
    runMultiThreaded();
}
