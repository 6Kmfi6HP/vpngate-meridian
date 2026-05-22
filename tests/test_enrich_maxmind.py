import base64
import importlib.util
import json
import sys
from pathlib import Path
from types import SimpleNamespace


SCRIPT_PATH = Path(__file__).resolve().parents[1] / 'scripts' / 'enrich_maxmind.py'
SPEC = importlib.util.spec_from_file_location('enrich_maxmind', SCRIPT_PATH)
enrich_maxmind = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = enrich_maxmind
SPEC.loader.exec_module(enrich_maxmind)


def encode_openvpn_config(config):
    return base64.b64encode(config.encode('utf-8')).decode('ascii')


class FakeReader:
    def __init__(self, value=None):
        self.value = value

    def city(self, ip):
        return self.value

    def country(self, ip):
        return self.value

    def asn(self, ip):
        return self.value


def named_record(**kwargs):
    return SimpleNamespace(**kwargs)


def test_annotate_servers_adds_supported_maxmind_fields():
    response = SimpleNamespace(
        country=named_record(iso_code='JP', name='Japan', names={'en': 'Japan'}, is_in_european_union=False),
        registered_country=named_record(iso_code='JP', name='Japan'),
        continent=named_record(code='AS', name='Asia'),
        city=named_record(name='Tokyo', geoname_id=1850147),
        subdivisions=SimpleNamespace(most_specific=named_record(iso_code='13', name='Tokyo')),
        location=SimpleNamespace(latitude=35.68, longitude=139.76, accuracy_radius=20, time_zone='Asia/Tokyo'),
        postal=SimpleNamespace(code='100-0001'),
        autonomous_system_number=2516,
        autonomous_system_organization='KDDI CORPORATION',
        network='203.0.113.0/24',
    )
    servers = [{'ip': '203.0.113.10', 'ipdata': {'old': True}}]

    stats = enrich_maxmind.annotate_servers_with_maxmind(
        servers,
        FakeReader(response),
        FakeReader(response),
        FakeReader(response),
    )

    assert stats == {'annotated': 1, 'failed': 0, 'skipped': 0}
    assert 'ipdata' not in servers[0]
    assert servers[0]['maxmind']['country']['iso_code'] == 'JP'
    assert servers[0]['maxmind']['city']['name'] == 'Tokyo'
    assert servers[0]['maxmind']['asn']['number'] == 2516
    assert 'risk_score' not in servers[0]['maxmind']


def test_build_mihomo_openvpn_config_decodes_vpngate_servers():
    data = {
        'data': {
            'servers': [
                {
                    'hostname': 'vpn.example.test',
                    'ip': '203.0.113.10',
                    'countryshort': 'JP',
                    'maxmind': {'asn': {'number': 2516}},
                    'openvpn_configdata_base64': encode_openvpn_config(
                        '''client
dev tun
proto tcp
remote 203.0.113.10 443
cipher AES-128-CBC
auth SHA1
<ca>
CA DATA
</ca>
<cert>
CERT DATA
</cert>
<key>
KEY DATA
</key>
'''
                    ),
                }
            ]
        }
    }

    config = enrich_maxmind.build_mihomo_openvpn_config(data)

    assert config == {
        'proxies': [
            {
                'name': 'JP AS2516 vpn.example.test',
                'type': 'openvpn',
                'dev': 'tun',
                'proto': 'tcp',
                'udp': False,
                'server': '203.0.113.10',
                'port': 443,
                'cipher': 'AES-128-CBC',
                'auth': 'SHA1',
                'ca': 'CA DATA',
                'cert': 'CERT DATA',
                'key': 'KEY DATA',
                'tls-crypt': '',
            }
        ]
    }


def test_render_mihomo_yaml_uses_block_scalars_and_quotes_strings():
    yaml = enrich_maxmind.render_mihomo_yaml(
        {
            'proxies': [
                {
                    'name': 'JP AS2516 vpn.example.test',
                    'type': 'openvpn',
                    'udp': False,
                    'port': 443,
                    'ca': 'line1\nline2',
                }
            ]
        }
    )

    assert yaml == '''proxies:
  - name: "JP AS2516 vpn.example.test"
    type: "openvpn"
    udp: false
    port: 443
    ca: |-
      line1
      line2
'''


def test_invalid_openvpn_config_is_skipped():
    data = {
        'data': {
            'servers': [
                {
                    'hostname': 'bad.example.test',
                    'openvpn_configdata_base64': 'not base64',
                }
            ]
        }
    }

    assert enrich_maxmind.build_mihomo_openvpn_config(data) == {'proxies': []}


def test_load_vpngate_data_rejects_unexpected_shape(tmp_path):
    input_file = tmp_path / 'bad.json'
    input_file.write_text(json.dumps({'data': {}}), encoding='utf-8')

    try:
        enrich_maxmind.load_vpngate_data(str(input_file))
    except ValueError as exc:
        assert 'data.servers' in str(exc)
    else:
        raise AssertionError('expected load_vpngate_data to reject missing servers')
