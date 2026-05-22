const axios = require("axios");
// const { SocksProxyAgent } = require('socks-proxy-agent');
const { generateRandomUserAgent, generateRandomCookie, generateRandomString } = require('../utils/randomizer');

class VpnScraper {
    constructor(proxyUrl = 'socks5://127.0.0.1:7890') {
        this.baseUrl = "http://www.vpngate.net/api/iphone/";
        // this.proxyAgent = new SocksProxyAgent(proxyUrl);
    }

    async fetchVpnData() {
        const randomParams = {
            t: Date.now(),
            nonce: generateRandomString(10),
            r: Math.random()
        };
        const queryString = new URLSearchParams(randomParams).toString();
        const vpnGateApiUrl = `${this.baseUrl}?${queryString}`;

        try {
            const { data } = await axios.get(vpnGateApiUrl, {
                headers: {
                    'User-Agent': generateRandomUserAgent(),
                    'Cookie': generateRandomCookie(),
                    'Accept': 'text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8',
                    'Accept-Language': 'en-US,en;q=0.5',
                    'Cache-Control': 'no-cache'
                },
                // httpsAgent: this.proxyAgent,
                // httpAgent: this.proxyAgent
            });

            if (!data) {
                throw new Error('No data returned from API');
            }

            return this.parseVpnData(data);
        } catch (error) {
            throw error;
        }
    }

    parseVpnData(data) {
        const lines = data.split("\n");
        if (lines.length < 2) {
            throw new Error('Invalid data format');
        }

        const headers = lines[1].split(",").map((header, index) => {
            return index === 0 ? header.slice(1) : header.split("\r")[0];
        });

        const servers = [];
        const countries = {};

        lines.slice(2, -2).forEach(vpn => {
            const values = vpn.split(",");
            countries[values[6].toLowerCase()] = values[5];
            
            const server = {};
            for (let j = 0; j < values.length; j++) {
                server[headers[j].toLowerCase()] = values[j];
            }
            servers.push(server);
        });

        return { servers, countries };
    }
}

module.exports = VpnScraper; 
