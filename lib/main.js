const axios = require("axios");
const { SocksProxyAgent } = require('socks-proxy-agent');

// Configure SOCKS5 proxy
const proxyAgent = new SocksProxyAgent('socks5://127.0.0.1:7890');

// Utility functions for generating random values
function generateRandomString(length = 8) {
    return Math.random().toString(36).substring(2, length + 2);
}

function generateRandomUserAgent() {
    const userAgents = [
        'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36',
        'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36',
        'Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:109.0) Gecko/20100101 Firefox/121.0',
        'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Safari/605.1.15',
        'Mozilla/5.0 (iPhone; CPU iPhone OS 17_2_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Mobile/15E148 Safari/604.1'
    ];
    return userAgents[Math.floor(Math.random() * userAgents.length)];
}

function generateRandomCookie() {
    const cookieId = generateRandomString(12);
    const sessionId = generateRandomString(16);
    return `vid=${cookieId}; sessionId=${sessionId}; visited=true`;
}

const getVpnList = () => {
    return new Promise(async (resolve, reject) => {
        const baseUrl = "http://www.vpngate.net/api/iphone/";
        // Add random query parameters
        const randomParams = {
            t: Date.now(),
            nonce: generateRandomString(10),
            r: Math.random()
        };
        const queryString = new URLSearchParams(randomParams).toString();
        const vpnGateApiUrl = `${baseUrl}?${queryString}`;

        let servers = []
        let headers = []
        let countries = {}
        let returnData = {servers, countries}
    
        try {
            let {data} = await axios.get(vpnGateApiUrl, {
                headers: {
                    'User-Agent': generateRandomUserAgent(),
                    'Cookie': generateRandomCookie(),
                    'Accept': 'text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8',
                    'Accept-Language': 'en-US,en;q=0.5',
                    'Cache-Control': 'no-cache'
                },
                httpsAgent: proxyAgent,
                httpAgent: proxyAgent
            });
            // if no data returned, return cache
            if(!data) reject({...returnData})
        
            // split lines
            data = data.split("\n")
            // if no data returned, return cache
            if(data.length<2) reject({...returnData})
        
            // get headers
            headers = data[1].split(",")
            headers[0] = headers[0].slice(1)
            let a = headers[headers.length-1]
            headers[headers.length-1] = a.split("\r")[0]
        
            // Clean up the data
            data = data.slice(2, data.length-2)
            
            // make object and store in list
            data.forEach(vpn => {
                let val = vpn.split(",")
                countries[val[6].toLowerCase()] = val[5]
                let obj = {}
                for(let j = 0; j < val.length; j++) {
                    obj[headers[j].toLowerCase()] = val[j];
                }
                servers.push(obj)
            })
            resolve({...returnData})
        } catch (error) {
            reject(error)
        }
    })
}

module.exports = getVpnList