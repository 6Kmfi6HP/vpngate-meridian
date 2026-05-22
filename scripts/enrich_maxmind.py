#!/usr/bin/env python3

import argparse
import base64
import binascii
import json
import logging
import os
import tempfile
from contextlib import ExitStack
from datetime import datetime, timezone
from typing import Any, Dict, Iterable, List, Optional
from urllib.parse import urlparse

import geoip2.database
from geoip2.errors import AddressNotFoundError, GeoIP2Error
import requests
from requests.adapters import HTTPAdapter
from urllib3.util.retry import Retry


DEFAULT_INPUT_FILE = os.getenv('VPNGATE_INPUT_FILE', 'public/json/data.json')
DEFAULT_OUTPUT_FILE = os.getenv('MAXMIND_OUTPUT_FILE', 'public/json/data.maxmind.json')
DEFAULT_MIHOMO_OUTPUT_FILE = os.getenv('MIHOMO_OUTPUT_FILE', 'public/mihomo_openvpn.yaml')
DEFAULT_MAXMIND_DB_DIR = os.getenv('MAXMIND_DB_DIR', 'maxmind')
RETRY_TOTAL = int(os.getenv('RETRY_TOTAL', '5'))
RETRY_BACKOFF_FACTOR = float(os.getenv('RETRY_BACKOFF_FACTOR', '1'))
REQUEST_TIMEOUT = float(os.getenv('REQUEST_TIMEOUT', '10'))

logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s [%(levelname)s] %(message)s',
    datefmt='%Y-%m-%d %H:%M:%S',
)
logger = logging.getLogger(__name__)


def create_session() -> requests.Session:
    session = requests.Session()
    retry = Retry(
        total=RETRY_TOTAL,
        backoff_factor=RETRY_BACKOFF_FACTOR,
        status_forcelist=[429, 500, 502, 503, 504],
        allowed_methods=['HEAD', 'GET', 'OPTIONS'],
    )
    adapter = HTTPAdapter(max_retries=retry)
    session.mount('http://', adapter)
    session.mount('https://', adapter)
    return session


def is_url(value: str) -> bool:
    parsed = urlparse(value)
    return parsed.scheme in {'http', 'https'} and bool(parsed.netloc)


def load_vpngate_data(input_path: str) -> Dict[str, Any]:
    if is_url(input_path):
        logger.info('Fetching VPNGate data from %s', input_path)
        session = create_session()
        try:
            response = session.get(input_path, timeout=REQUEST_TIMEOUT * 1.5)
            response.raise_for_status()
            data = response.json()
        finally:
            session.close()
    else:
        logger.info('Reading VPNGate data from %s', input_path)
        with open(input_path, 'r', encoding='utf-8') as handle:
            data = json.load(handle)

    validate_vpngate_data(data)
    return data


def validate_vpngate_data(data: Dict[str, Any]) -> None:
    if not isinstance(data, dict):
        raise ValueError('VPNGate data must be a JSON object')
    if not isinstance(data.get('data'), dict):
        raise ValueError('VPNGate data is missing the data object')
    if not isinstance(data['data'].get('servers'), list):
        raise ValueError('VPNGate data is missing data.servers')


def compact_dict(data: Dict[str, Any]) -> Dict[str, Any]:
    return {key: value for key, value in data.items() if value is not None}


def names_record(record: Any) -> Dict[str, Any]:
    return compact_dict(
        {
            'iso_code': getattr(record, 'iso_code', None),
            'name': getattr(record, 'name', None),
            'names': getattr(record, 'names', None) or None,
            'is_in_european_union': getattr(record, 'is_in_european_union', None),
        }
    )


def continent_record(record: Any) -> Dict[str, Any]:
    return compact_dict(
        {
            'code': getattr(record, 'code', None),
            'name': getattr(record, 'name', None),
            'names': getattr(record, 'names', None) or None,
        }
    )


def city_record(record: Any) -> Dict[str, Any]:
    return compact_dict(
        {
            'name': getattr(record, 'name', None),
            'names': getattr(record, 'names', None) or None,
            'geoname_id': getattr(record, 'geoname_id', None),
        }
    )


def subdivision_record(record: Any) -> Dict[str, Any]:
    return compact_dict(
        {
            'iso_code': getattr(record, 'iso_code', None),
            'name': getattr(record, 'name', None),
            'names': getattr(record, 'names', None) or None,
            'geoname_id': getattr(record, 'geoname_id', None),
        }
    )


def lookup(reader: Any, method: str, ip: str) -> Optional[Any]:
    try:
        return getattr(reader, method)(ip)
    except AddressNotFoundError:
        return None


def build_maxmind_record(ip: str, city_reader: Any, country_reader: Any, asn_reader: Any) -> Dict[str, Any]:
    country_response = lookup(country_reader, 'country', ip)
    city_response = lookup(city_reader, 'city', ip)
    asn_response = lookup(asn_reader, 'asn', ip)

    response = city_response or country_response
    record: Dict[str, Any] = {}

    if response:
        country = names_record(getattr(response, 'country', None))
        registered_country = names_record(getattr(response, 'registered_country', None))
        continent = continent_record(getattr(response, 'continent', None))
        city = city_record(getattr(response, 'city', None))
        subdivision = subdivision_record(
            getattr(getattr(response, 'subdivisions', None), 'most_specific', None)
        )
        location_obj = getattr(response, 'location', None)
        location = compact_dict(
            {
                'latitude': getattr(location_obj, 'latitude', None),
                'longitude': getattr(location_obj, 'longitude', None),
                'accuracy_radius': getattr(location_obj, 'accuracy_radius', None),
                'time_zone': getattr(location_obj, 'time_zone', None),
            }
        )
        postal = compact_dict({'code': getattr(getattr(response, 'postal', None), 'code', None)})

        if country:
            record['country'] = country
        if registered_country:
            record['registered_country'] = registered_country
        if continent:
            record['continent'] = continent
        if city:
            record['city'] = city
        if subdivision:
            record['subdivision'] = subdivision
        if location:
            record['location'] = location
        if postal:
            record['postal'] = postal

    if asn_response:
        network = getattr(asn_response, 'network', None)
        asn = compact_dict(
            {
                'number': getattr(asn_response, 'autonomous_system_number', None),
                'organization': getattr(asn_response, 'autonomous_system_organization', None),
                'network': str(network) if network else None,
            }
        )
        if asn:
            record['asn'] = asn

    return record


def annotate_servers_with_maxmind(
    servers: Iterable[Dict[str, Any]], city_reader: Any, country_reader: Any, asn_reader: Any
) -> Dict[str, int]:
    stats = {'annotated': 0, 'failed': 0, 'skipped': 0}
    for server in servers:
        server.pop('ipdata', None)
        server.pop('maxmind', None)
        ip = server.get('ip')
        if not ip:
            stats['skipped'] += 1
            continue
        try:
            record = build_maxmind_record(ip, city_reader, country_reader, asn_reader)
        except (GeoIP2Error, ValueError) as exc:
            logger.warning('IP %s lookup failed: %s', ip, exc)
            stats['failed'] += 1
            continue
        if record:
            server['maxmind'] = record
            stats['annotated'] += 1
        else:
            stats['failed'] += 1
    return stats


def decode_openvpn_config(encoded_config: str) -> str:
    return base64.b64decode(encoded_config.strip(), validate=True).decode('utf-8')


def extract_openvpn_block(config: str, tag: str) -> Optional[str]:
    start_marker = f'<{tag}>'
    end_marker = f'</{tag}>'
    start = config.find(start_marker)
    end = config.find(end_marker)
    if start == -1 or end == -1 or end < start:
        return None
    return config[start + len(start_marker) : end].strip()


def parse_openvpn_config(config: str) -> Dict[str, Any]:
    proxy: Dict[str, Any] = {}
    for line in config.splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith('#') or stripped.startswith(';'):
            continue
        parts = stripped.split()
        if parts[0] == 'proto' and len(parts) >= 2:
            proxy['proto'] = parts[1]
            proxy['udp'] = parts[1].lower().startswith('udp')
        elif parts[0] in {'dev', 'cipher', 'auth'} and len(parts) >= 2:
            proxy[parts[0]] = parts[1]
        elif parts[0] == 'remote' and len(parts) >= 3:
            port = int(parts[2])
            if port < 1 or port > 65535:
                raise ValueError(f'invalid OpenVPN remote port: {port}')
            proxy['server'] = parts[1]
            proxy['port'] = port

    for tag in ('ca', 'cert', 'key', 'tls-crypt'):
        value = extract_openvpn_block(config, tag)
        if value:
            proxy[tag] = value

    return proxy


def build_mihomo_proxy_name(server: Dict[str, Any]) -> str:
    base_name = str(server.get('name') or server.get('hostname') or server.get('ip'))
    country_iso = server.get('countryshort') or server.get('maxmind', {}).get('country', {}).get('iso_code')
    asn_number = server.get('maxmind', {}).get('asn', {}).get('number')
    parts = []
    if country_iso:
        parts.append(str(country_iso))
    if asn_number:
        parts.append(f'AS{asn_number}')
    parts.append(base_name)
    return ' '.join(parts)


def build_mihomo_openvpn_proxy(server: Dict[str, Any]) -> Optional[Dict[str, Any]]:
    encoded_config = server.get('openvpn_configdata_base64')
    if not encoded_config:
        return None

    try:
        proxy = parse_openvpn_config(decode_openvpn_config(encoded_config))
    except (binascii.Error, UnicodeDecodeError, ValueError) as exc:
        logger.warning('OpenVPN config for %s decode failed: %s', server.get('hostname') or server.get('ip'), exc)
        return None
    if 'server' not in proxy or 'port' not in proxy:
        return None
    proxy.setdefault('tls-crypt', '')

    return {
        'name': build_mihomo_proxy_name(server),
        'type': 'openvpn',
        **proxy,
    }


def build_mihomo_openvpn_config(vpn_data: Dict[str, Any]) -> Dict[str, List[Dict[str, Any]]]:
    proxies = []
    for server in vpn_data.get('data', {}).get('servers', []):
        proxy = build_mihomo_openvpn_proxy(server)
        if proxy:
            proxies.append(proxy)
    return {'proxies': proxies}


def format_yaml_scalar(value: Any) -> str:
    if isinstance(value, bool):
        return 'true' if value else 'false'
    if isinstance(value, str):
        return json.dumps(value, ensure_ascii=False)
    return str(value)


def render_mihomo_yaml(config: Dict[str, List[Dict[str, Any]]]) -> str:
    lines = ['proxies:']
    for proxy in config.get('proxies', []):
        items = list(proxy.items())
        for index, (key, value) in enumerate(items):
            prefix = '  -' if index == 0 else '   '
            if isinstance(value, str) and '\n' in value:
                lines.append(f'{prefix} {key}: |-')
                lines.extend(f'      {line}' for line in value.splitlines())
            else:
                lines.append(f'{prefix} {key}: {format_yaml_scalar(value)}')
    return '\n'.join(lines) + '\n'


def save_json_atomic(data: Dict[str, Any], path: str) -> None:
    dirpath = os.path.dirname(path) or '.'
    os.makedirs(dirpath, exist_ok=True)
    with tempfile.NamedTemporaryFile('w', delete=False, dir=dirpath, encoding='utf-8') as temp_file:
        json.dump(data, temp_file, indent=2, ensure_ascii=False)
        temp_file.write('\n')
        temp_path = temp_file.name
    os.replace(temp_path, path)
    logger.info('Output saved to %s', path)


def save_text_atomic(text: str, path: str) -> None:
    dirpath = os.path.dirname(path) or '.'
    os.makedirs(dirpath, exist_ok=True)
    with tempfile.NamedTemporaryFile('w', delete=False, dir=dirpath, encoding='utf-8') as temp_file:
        temp_file.write(text)
        temp_path = temp_file.name
    os.replace(temp_path, path)
    logger.info('Output saved to %s', path)


def enrich_vpngate_data(
    vpn_data: Dict[str, Any], country_db: str, city_db: str, asn_db: str
) -> Dict[str, Any]:
    validate_vpngate_data(vpn_data)
    servers = vpn_data['data']['servers']
    with ExitStack() as stack:
        country_reader = stack.enter_context(geoip2.database.Reader(country_db))
        city_reader = stack.enter_context(geoip2.database.Reader(city_db))
        asn_reader = stack.enter_context(geoip2.database.Reader(asn_db))
        stats = annotate_servers_with_maxmind(servers, city_reader, country_reader, asn_reader)

    vpn_data['maxmind'] = {
        'generatedAt': datetime.now(timezone.utc).isoformat(timespec='seconds').replace('+00:00', 'Z'),
        'statistics': stats,
        'databases': {
            'country': os.path.basename(country_db),
            'city': os.path.basename(city_db),
            'asn': os.path.basename(asn_db),
        },
    }
    return vpn_data


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description='Enrich VPNGate output with MaxMind data and export mihomo OpenVPN YAML.')
    parser.add_argument('--input', default=DEFAULT_INPUT_FILE, help='Input VPNGate data.json path or HTTPS URL.')
    parser.add_argument('--output', default=DEFAULT_OUTPUT_FILE, help='Output MaxMind-enriched JSON path.')
    parser.add_argument('--mihomo-output', default=DEFAULT_MIHOMO_OUTPUT_FILE, help='Output mihomo OpenVPN YAML path.')
    parser.add_argument('--maxmind-dir', default=DEFAULT_MAXMIND_DB_DIR, help='Directory containing GeoLite2 mmdb files.')
    parser.add_argument('--country-db', default=None, help='GeoLite2-Country.mmdb path.')
    parser.add_argument('--city-db', default=None, help='GeoLite2-City.mmdb path.')
    parser.add_argument('--asn-db', default=None, help='GeoLite2-ASN.mmdb path.')
    parser.add_argument('--skip-mihomo', action='store_true', help='Only write the enriched JSON output.')
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    country_db = args.country_db or os.path.join(args.maxmind_dir, 'GeoLite2-Country.mmdb')
    city_db = args.city_db or os.path.join(args.maxmind_dir, 'GeoLite2-City.mmdb')
    asn_db = args.asn_db or os.path.join(args.maxmind_dir, 'GeoLite2-ASN.mmdb')

    vpn_data = load_vpngate_data(args.input)
    server_count = len(vpn_data['data']['servers'])
    logger.info('Found %d servers', server_count)

    enriched_data = enrich_vpngate_data(vpn_data, country_db, city_db, asn_db)
    save_json_atomic(enriched_data, args.output)

    if not args.skip_mihomo:
        save_text_atomic(render_mihomo_yaml(build_mihomo_openvpn_config(enriched_data)), args.mihomo_output)


if __name__ == '__main__':
    main()
