#!/usr/bin/env python3
"""Export selected, sanitized typed measurements from a successful native report.

This does not execute Podman, read configuration, or capture arbitrary stdout.
The info document is reconstructed from published typed fields, not a verbatim
command transcript. Replay re-runs its parser and the prerequisite evaluators.
"""
import argparse
import json
from pathlib import Path
import re


MAX_REPORT_BYTES = 2 << 20


def redact(value):
    if isinstance(value, str):
        return re.sub(r'/home/[^/\s]+', '/home/captured-user', value)
    if isinstance(value, list):
        return [redact(item) for item in value]
    if isinstance(value, dict):
        return {key: ('captured-user' if key == 'username' and item not in ('', 'root') else redact(item))
                for key, item in value.items()}
    return value


def project(report, description, configuration=False):
    trace, runtime = report['evaluation'], report['runtimes']['podman']
    if trace['mode'] != 'live' or trace['collection'] != 'active' or runtime['accessible'] is not True:
        raise ValueError('capture requires completed native local inspection')
    observations, selected = [], {}
    for original in trace['observations']:
        if 'executable' in original:
            executable = original['executable']
            if executable['source'] == 'runtime':
                selected[executable['role']] = executable['selected_path']
        configured = configuration and ('configuration' in original or 'storage_paths' in original or
                                        original['probe_id'] == 'podman.version' or 'filesystems' in original.get('host', {}))
        if not configured and not any(key in original for key in ('executable', 'quadlet', 'user_context')) and not any(
                key in original.get('host', {}) for key in ('cgroups', 'systemd')):
            continue
        # Wire fact references omit raw values and their shared scope. Restore
        # the owning observation's scope without manufacturing file contents.
        observation = dict(original)
        observation['facts'] = [dict(fact, scope=original['scope']) for fact in original['facts']]
        if configuration and original['probe_id'] == 'podman.version':
            observation['version'] = {'Path': runtime['path'], 'Runnable': runtime['cli_runnable'],
                                      'Version': runtime['version_details']}
        observations.append(observation)
    info = {'version': {'Version': runtime['version']}, 'host': {
        'security': {'rootless': runtime['rootless']}, 'serviceIsRemote': False,
        'cgroupVersion': runtime.get('cgroup_version'), 'cgroupManager': runtime.get('cgroup_manager'),
        'networkBackend': runtime.get('network_backend'),
        'rootlessNetworkCmd': runtime.get('rootless_network_cmd'),
        'networkBackendInfo': {'path': selected.get('netavark', ''), 'dns': {'path': selected.get('aardvark_dns', '')}},
        'conmon': {'path': selected.get('conmon', '')}, 'pasta': {'executable': selected.get('pasta', '')},
        'slirp4netns': {'executable': selected.get('slirp4netns', '')},
        'ociRuntime': runtime.get('oci_runtime')},
        'store': {'graphDriverName': runtime.get('storage_driver'), 'graphRoot': runtime.get('graph_root'),
                  'runRoot': runtime.get('run_root')}}
    if selected.get('storage_mount_program'):
        info['store']['graphOptions'] = {'overlay.mount_program': {'Executable': selected['storage_mount_program']}}
    states = {key: value['state'] for key, value in report['capabilities'].items()
              if key.startswith(('runtime.podman.helper.', 'runtime.podman.quadlet.'))
              or key.endswith('.executable') or key == 'runtime.podman.cgroup_v2'}
    if configuration:
        for key in ('runtime.podman.config.engine.parsed', 'runtime.podman.cgroup_manager.systemd'):
            states[key] = report['capabilities'][key]['state']
        states.update({key: value['state'] for key, value in report['capabilities'].items()
                       if key.startswith('runtime.podman.storage.') or key == 'runtime.podman.config.storage.parsed'})
        states.update({key: value['state'] for key, value in report['capabilities'].items()
                       if key.startswith('runtime.podman.network.') or key in
                       ('runtime.podman.config.network.parsed', 'runtime.podman.config.dns.parsed')})
    result = redact({'schema_version': 1, 'provenance': {'kind': 'captured', 'description': description,
                     'conversion': 'Typed native measurements; info JSON reconstructed from published fields; home and user names anonymized.'},
                     'scope': trace['scope'], 'timestamp': trace['timestamp'], 'runtime_path': runtime['path'],
                     'context': {'id': trace['scope']['context_id'], 'identity': {
                         'current': trace['current'], 'target': trace['target'], 'execution': trace['execution']}},
                     'info': info, 'observations': observations, 'states': states})
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--report', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--description', required=True)
    parser.add_argument('--configuration', action='store_true', help='include typed configuration sources, selected paths and their version evidence')
    args = parser.parse_args()
    data = args.report.read_bytes()
    if len(data) > MAX_REPORT_BYTES:
        raise ValueError('report exceeds capture bound')
    result = project(json.loads(data), args.description, configuration=args.configuration)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(result, indent=2) + '\n')


if __name__ == '__main__':
    main()
