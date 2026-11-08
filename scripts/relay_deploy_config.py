"""Offline rendering for explicitly authorized Relay deployments; never connects."""
import ipaddress
import re
import shlex


def relay_unit(trusted_proxies):
    if not trusted_proxies:
        raise ValueError('Explicit trusted proxy addresses are required')
    networks = []
    for value in trusted_proxies:
        # Validate the original address before masking; ip_network(strict=False)
        # would otherwise erase the mapped marker on prefixes shorter than /96.
        if not isinstance(value,str) or not re.fullmatch(r'[0-9a-fA-F:.]+(?:/[0-9]{1,3})?',value):
            raise ValueError('Trusted proxy must be an IP/CIDR without zones, whitespace or controls')
        host, slash, bits = value.partition('/')
        address = ipaddress.ip_address(host)
        prefixlen = int(bits) if slash else address.max_prefixlen
        if not 0 < prefixlen <= address.max_prefixlen:
            raise ValueError('Invalid or universal trusted prefix')
        if isinstance(address,ipaddress.IPv6Address) and address.ipv4_mapped:
            if prefixlen <= 96:
                raise ValueError('Mapped trusted prefix must be narrower than /96')
            address, prefixlen = address.ipv4_mapped, prefixlen-96
        network = ipaddress.ip_network((address,prefixlen),strict=False)
        networks.append(str(network))
    argv = ['/opt/xallor-remote/bin/xallor-remote','relay','--listen','0.0.0.0:8443',
            '--reverse-proxy','--trusted-proxy','127.0.0.1']
    for value in sorted(set(networks)):
        argv.extend(['--trusted-proxy',value])
    argv.extend(['--data','/var/lib/xallor-remote','--quota'])
    return '''[Unit]
Description=XallorRemote Relay
After=network.target

[Service]
Type=simple
ExecStart='''+shlex.join(argv)+'''
Restart=on-failure
RestartSec=2

[Install]
WantedBy=multi-user.target
'''
