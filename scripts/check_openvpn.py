#!/usr/bin/env python3

"""
Test OpenVPN servers using mihomo's Go OpenVPN implementation in-process.

Uses the ``scripts/tester`` Go binary (inspired by subs-check's approach) to
test proxies concurrently via mihomo's Go library -- no Docker, no external
API, no batch processing. Each proxy connection is created in-process using
``adapter.ParseProxy`` + ``proxy.DialContext`` and wrapped in an ``http.Client``.

Output files:
  --tested-data    data.json with a ``test`` field on each server + top-level
                   ``test`` metadata section
  --alive-mihomo   mihomo YAML containing only alive proxies, sorted by latency,
                   with ``# latency: XXXms`` comments
  --results-json   summary JSON with all statistics and per-proxy results
"""

import argparse
import json
import logging
import os
import subprocess
import sys
import tempfile
from datetime import datetime, timezone
from typing import Any, Dict, List, Optional, Tuple

import yaml

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------

DEFAULT_MIHOMO_INPUT = os.getenv(
    'MIHOMO_INPUT_FILE', 'public/mihomo_openvpn.yaml'
)
DEFAULT_DATA_INPUT = os.getenv(
    'TEST_DATA_INPUT_FILE', 'public/json/data.json'
)
DEFAULT_TESTED_DATA = os.getenv(
    'TESTED_DATA_FILE', 'public/json/data.tested.json'
)
DEFAULT_ALIVE_MIHOMO = os.getenv(
    'ALIVE_MIHOMO_FILE', 'public/mihomo_tested_openvpn.yaml'
)
DEFAULT_RESULTS_JSON = os.getenv(
    'RESULTS_JSON_FILE', 'public/json/test_results.json'
)
DEFAULT_MAX_ALIVE = int(os.getenv('TEST_MAX_ALIVE', '100'))
DEFAULT_TIMEOUT = int(os.getenv('TEST_TIMEOUT', '10'))
DEFAULT_WORKERS = int(os.getenv('TEST_WORKERS', '20'))

TESTER_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'tester')
TESTER_BINARY = os.path.join(TESTER_DIR, 'tester')

logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s [%(levelname)s] %(message)s',
    datefmt='%Y-%m-%d %H:%M:%S',
)
logger = logging.getLogger(__name__)


# ---------------------------------------------------------------------------
# Go tester helpers
# ---------------------------------------------------------------------------

def _check_go() -> bool:
    """Return True if Go toolchain is available."""
    try:
        result = subprocess.run(
            ['go', 'version'],
            capture_output=True,
            text=True,
            timeout=10,
        )
        return result.returncode == 0
    except (subprocess.SubprocessError, FileNotFoundError):
        return False


def ensure_tester() -> Optional[str]:
    """Return path to Go tester binary, building it if necessary.

    Returns None when neither a prebuilt binary nor the Go toolchain is
    available (caller should skip testing gracefully).
    """
    if os.path.isfile(TESTER_BINARY) and os.access(TESTER_BINARY, os.X_OK):
        return TESTER_BINARY

    if not _check_go():
        logger.warning(
            'Go toolchain is not available and no prebuilt tester binary '
            'found. Skipping OpenVPN testing.'
        )
        return None

    logger.info('Building Go tester binary from %s ...', TESTER_DIR)
    result = subprocess.run(
        ['go', 'build', '-tags', 'with_gvisor', '-o', TESTER_BINARY, '.'],
        cwd=TESTER_DIR,
        capture_output=True,
        text=True,
        timeout=300,  # first build may download many dependencies
    )
    if result.returncode != 0:
        logger.warning(
            'Failed to build Go tester (exit %d): %s',
            result.returncode, result.stderr.strip(),
        )
        return None

    logger.info('Go tester binary built successfully')
    return TESTER_BINARY


def run_tester(
    tester_path: str,
    input_yaml: str,
    timeout_sec: int,
    workers: int,
) -> Optional[Dict[str, Any]]:
    """Run the Go tester, returning parsed JSON output or None on failure."""
    logger.info(
        'Running Go tester: --input %s --timeout %d --workers %d',
        input_yaml, timeout_sec, workers,
    )
    try:
        result = subprocess.run(
            [
                tester_path,
                '--input', input_yaml,
                '--timeout', str(timeout_sec),
                '--workers', str(workers),
            ],
            capture_output=True,
            text=True,
            timeout=max(600, timeout_sec * 4),
        )
    except subprocess.TimeoutExpired:
        logger.warning('Go tester timed out')
        return None

    if result.returncode != 0:
        logger.warning(
            'Go tester failed (exit %d): %s',
            result.returncode, result.stderr.strip(),
        )
        return None

    # Log Go tester stderr (progress + error breakdown) for CI diagnostics.
    if result.stderr.strip():
        for line in result.stderr.strip().splitlines():
            logger.info('[tester] %s', line)

    try:
        return json.loads(result.stdout)
    except json.JSONDecodeError as exc:
        logger.warning('Failed to parse Go tester output: %s', exc)
        return None


# ---------------------------------------------------------------------------
# Server matching
# ---------------------------------------------------------------------------

def extract_hostname(proxy_name: str) -> str:
    """Extract hostname from a proxy name like ``COUNTRY AS<num> hostname``."""
    return proxy_name.rsplit(' ', 1)[-1] if ' ' in proxy_name else proxy_name


def build_proxy_to_server_map(
    servers: List[Dict[str, Any]],
    proxy_names: List[str],
) -> Dict[str, int]:
    """Map each proxy name to its server index by matching hostname."""
    hostname_to_idx: Dict[str, int] = {}
    for idx, server in enumerate(servers):
        hostname = (
            server.get('hostname')
            or server.get('name')
            or server.get('ip')
        )
        if hostname:
            hostname_to_idx[hostname] = idx

    proxy_to_server: Dict[str, int] = {}
    for pname in proxy_names:
        hostname = extract_hostname(pname)
        idx = hostname_to_idx.get(hostname)
        if idx is not None:
            proxy_to_server[pname] = idx
        else:
            logger.warning(
                'Could not match proxy %r to any server (hostname=%r)',
                pname, hostname,
            )
    return proxy_to_server


# ---------------------------------------------------------------------------
# YAML rendering for alive proxies
# ---------------------------------------------------------------------------

def render_alive_yaml(
    proxies_yaml: str,
    alive_names: Dict[str, int],
    max_alive: int,
) -> str:
    """Render a YAML string with only alive proxies, sorted by latency.

    *proxies_yaml* is the raw content of the input mihomo YAML.  *alive_names*
    maps proxy name -> latency_ms.  Only the top *max_alive* fastest proxies
    are included, with latency comments.
    """
    raw = yaml.safe_load(proxies_yaml)
    if not isinstance(raw, dict) or 'proxies' not in raw:
        return 'proxies: []\n'

    all_proxies: List[dict] = raw['proxies']
    # Sort by latency (ascending), keep only alive ones
    alive_proxies = [
        p for p in all_proxies if p.get('name') in alive_names
    ]
    alive_proxies.sort(key=lambda p: alive_names[p['name']])
    if max_alive > 0:
        alive_proxies = alive_proxies[:max_alive]

    lines = ['proxies:']
    for proxy in alive_proxies:
        name = proxy['name']
        latency = alive_names[name]
        lines.append(f'  # latency: {latency}ms')
        for i, (key, value) in enumerate(proxy.items()):
            prefix = '  -' if i == 0 else '   '
            if isinstance(value, str) and '\n' in value:
                lines.append(f'{prefix} {key}: |-')
                for line in value.splitlines():
                    lines.append(f'      {line}')
            elif isinstance(value, bool):
                lines.append(f'{prefix} {key}: {"true" if value else "false"}')
            elif value is None:
                lines.append(f'{prefix} {key}: null')
            elif isinstance(value, (int, float)):
                lines.append(f'{prefix} {key}: {value}')
            else:
                lines.append(f'{prefix} {key}: {json.dumps(value, ensure_ascii=False)}')
    lines.append('')
    return '\n'.join(lines)


# ---------------------------------------------------------------------------
# Statistics
# ---------------------------------------------------------------------------

def compute_statistics(delays: List[int], total: int) -> Dict[str, Any]:
    """Compute summary latency statistics."""
    alive = len(delays)
    dead = total - alive
    alive_rate = round(alive / total * 100, 1) if total > 0 else 0.0

    stats: Dict[str, Any] = {
        'total': total,
        'tested': total,
        'alive': alive,
        'dead': dead,
        'aliveRate': alive_rate,
    }

    if delays:
        sorted_delays = sorted(delays)
        stats['avgLatency'] = round(
            sum(sorted_delays) / len(sorted_delays), 1
        )
        stats['p50Latency'] = sorted_delays[len(sorted_delays) // 2]
        p90_idx = max(
            0, min(len(sorted_delays) - 1, int(len(sorted_delays) * 0.9))
        )
        stats['p90Latency'] = sorted_delays[p90_idx]

    return stats


# ---------------------------------------------------------------------------
# Atomic file helpers
# ---------------------------------------------------------------------------

def save_json_atomic(data: Any, path: str) -> None:
    """Write JSON using atomic tempfile + rename."""
    dirpath = os.path.dirname(path) or '.'
    os.makedirs(dirpath, exist_ok=True)
    with tempfile.NamedTemporaryFile(
        'w', delete=False, dir=dirpath, encoding='utf-8',
    ) as f:
        json.dump(data, f, indent=2, ensure_ascii=False)
        f.write('\n')
        temp_path = f.name
    os.replace(temp_path, path)
    logger.info('Output saved to %s', path)


def save_text_atomic(text: str, path: str) -> None:
    """Write text using atomic tempfile + rename."""
    dirpath = os.path.dirname(path) or '.'
    os.makedirs(dirpath, exist_ok=True)
    with tempfile.NamedTemporaryFile(
        'w', delete=False, dir=dirpath, encoding='utf-8',
    ) as f:
        f.write(text)
        temp_path = f.name
    os.replace(temp_path, path)
    logger.info('Output saved to %s', path)


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

def parse_args(argv: Optional[List[str]] = None) -> argparse.Namespace:
    """Parse command-line arguments."""
    parser = argparse.ArgumentParser(
        description=(
            'Test OpenVPN servers using mihomo Go library in-process. '
            'No Docker required. Calls the scripts/tester Go binary '
            'which embeds mihomo via adapter.ParseProxy + proxy.DialContext.'
        ),
    )
    parser.add_argument(
        '--mihomo-input',
        default=DEFAULT_MIHOMO_INPUT,
        help=f'Input mihomo OpenVPN YAML (default: {DEFAULT_MIHOMO_INPUT})',
    )
    parser.add_argument(
        '--data-input',
        default=DEFAULT_DATA_INPUT,
        help=f'Input VPN Gate data.json (default: {DEFAULT_DATA_INPUT})',
    )
    parser.add_argument(
        '--tested-data',
        default=DEFAULT_TESTED_DATA,
        help=f'Output data.json annotated with test field (default: {DEFAULT_TESTED_DATA})',
    )
    parser.add_argument(
        '--alive-mihomo',
        default=DEFAULT_ALIVE_MIHOMO,
        help=f'Output YAML with alive proxies sorted by latency (default: {DEFAULT_ALIVE_MIHOMO})',
    )
    parser.add_argument(
        '--results-json',
        default=DEFAULT_RESULTS_JSON,
        help=f'Output JSON with statistics and per-proxy results (default: {DEFAULT_RESULTS_JSON})',
    )
    parser.add_argument(
        '--max-alive',
        type=int,
        default=DEFAULT_MAX_ALIVE,
        help=f'Max alive proxies in --alive-mihomo output, 0 for no limit (default: {DEFAULT_MAX_ALIVE})',
    )
    parser.add_argument(
        '--timeout',
        type=int,
        default=DEFAULT_TIMEOUT,
        help=f'Per-proxy test timeout in seconds (default: {DEFAULT_TIMEOUT})',
    )
    parser.add_argument(
        '--workers',
        type=int,
        default=DEFAULT_WORKERS,
        help=f'Concurrent test workers (default: {DEFAULT_WORKERS})',
    )
    return parser.parse_args(argv)


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def main(argv: Optional[List[str]] = None) -> None:
    """Entry point -- ensures the Go tester is available, runs it, and
    annotates ``data.json`` with test results."""
    args = parse_args(argv)

    # -- Ensure Go tester is available -------------------------------------
    tester_path = ensure_tester()
    if tester_path is None:
        logger.warning(
            'Go tester is not available (no Go toolchain and no prebuilt '
            'binary). Skipping OpenVPN testing.'
        )
        sys.exit(0)

    # -- Read input files --------------------------------------------------
    logger.info('Reading mihomo YAML from %s', args.mihomo_input)
    with open(args.mihomo_input, 'r', encoding='utf-8') as f:
        yaml_text = f.read()

    logger.info('Reading VPN Gate data from %s', args.data_input)
    with open(args.data_input, 'r', encoding='utf-8') as f:
        vpn_data = json.load(f)

    servers: List[Dict[str, Any]] = (
        vpn_data.get('data', {}).get('servers', [])
    )
    logger.info('Loaded %d servers from data.json', len(servers))

    # -- Run Go tester -----------------------------------------------------
    tester_output = run_tester(
        tester_path,
        args.mihomo_input,
        timeout_sec=args.timeout,
        workers=args.workers,
    )

    if tester_output is None:
        logger.error('Go tester produced no output. Aborting.')
        sys.exit(1)

    # Extract results
    go_stats = tester_output.get('statistics', {})
    go_results = tester_output.get('results', [])
    logger.info(
        'Go tester results: %d total, %d alive, %d dead',
        go_stats.get('total', 0),
        go_stats.get('alive', 0),
        go_stats.get('dead', 0),
    )

    # Build lookup: alive proxy name -> latency_ms
    alive_map: Dict[str, int] = {}
    all_delays: List[int] = []
    for r in go_results:
        name = r.get('name', '')
        if r.get('alive') and r.get('latencyMs', 0) > 0:
            alive_map[name] = r['latencyMs']
            all_delays.append(r['latencyMs'])

    # -- Build proxy-name to server-index mapping --------------------------
    proxy_names = [r.get('name', '') for r in go_results if r.get('name')]
    proxy_to_server = build_proxy_to_server_map(servers, proxy_names)

    # -- Timestamp ---------------------------------------------------------
    now_iso = (
        datetime.now(timezone.utc)
        .isoformat(timespec='milliseconds')
        .replace('+00:00', 'Z')
    )

    # -- Annotate servers --------------------------------------------------
    for r in go_results:
        name = r.get('name', '')
        server_idx = proxy_to_server.get(name)
        if server_idx is None:
            continue

        test_entry: Dict[str, Any] = {
            'alive': bool(r.get('alive')),
            'testedAt': now_iso,
        }
        if r.get('latencyMs'):
            test_entry['latencyMs'] = r['latencyMs']
        if r.get('error'):
            test_entry['error'] = r['error']
        servers[server_idx]['test'] = test_entry

    # -- Statistics --------------------------------------------------------
    stats = compute_statistics(all_delays, len(go_results))

    # -- Top-level test metadata in data.json ------------------------------
    vpn_data['test'] = {
        'generatedAt': now_iso,
        'statistics': stats,
    }

    # -- Save outputs ------------------------------------------------------

    # 1. Tested data.json
    save_json_atomic(vpn_data, args.tested_data)

    # 2. Results JSON (all results sorted: alive first by latency, then dead)
    all_results_sorted = sorted(
        go_results,
        key=lambda x: (
            not x.get('alive', False),
            x.get('latencyMs', 999999) if x.get('alive') else 0,
        ),
    )
    # Count error categories for diagnostics
    error_counts: Dict[str, int] = {}
    for r in go_results:
        err = r.get('error', '') or ''
        category = err.split(':')[0] if err else 'no_error'
        error_counts[category] = error_counts.get(category, 0) + 1

    results_data: Dict[str, Any] = {
        'generatedAt': now_iso,
        'statistics': stats,
        'errorCategories': sorted(error_counts.items(), key=lambda x: -x[1]),
        'results': [
            {
                'proxyName': r['name'],
                'alive': r.get('alive', False),
                'latencyMs': r.get('latencyMs'),
                'error': r.get('error'),
            }
            for r in all_results_sorted
        ],
    }
    save_json_atomic(results_data, args.results_json)

    # 3. Alive mihomo YAML (only alive, sorted by latency, limited)
    alive_yaml = render_alive_yaml(yaml_text, alive_map, args.max_alive)
    save_text_atomic(alive_yaml, args.alive_mihomo)

    # -- Summary to log ----------------------------------------------------
    logger.info(
        'Results: %d total, %d alive, %d dead (%.1f%% alive rate)',
        stats['total'], stats['alive'], stats['dead'], stats['aliveRate'],
    )
    if all_delays:
        logger.info(
            'Latency: avg=%.0fms  p50=%dms  p90=%dms',
            stats['avgLatency'], stats['p50Latency'], stats['p90Latency'],
        )


if __name__ == '__main__':
    main()
