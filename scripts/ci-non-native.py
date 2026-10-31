#!/usr/bin/env python3
"""Run the closure CI selection; CLOSE-nativo-029 withdraws native crash checks."""

import json
import pathlib
import re
import subprocess
import sys

root = pathlib.Path(__file__).resolve().parent.parent
inventory = json.loads((root / 'scripts/ci-test-inventory.json').read_text())


def output(*command):
    return subprocess.check_output(command, cwd=root, text=True)


def run(*command):
    print('+', ' '.join(command), flush=True)
    subprocess.run(command, cwd=root, check=True)


def verify_inventory():
    packages = set(output('go', 'list', '-tags=integration', './...').splitlines())
    selected = set(inventory['packages'])
    excluded = set(inventory['excludedPackages'])
    if selected & excluded or packages != selected | excluded:
        raise SystemExit('Package inventory changed; classify it before running CI.')

    for package, entry in inventory['packages'].items():
        listing = output('go', 'test', '-tags=integration', '-list', '^(Test|Fuzz|Example)', package)
        found = sorted(line for line in listing.splitlines() if line.startswith(('Test', 'Fuzz', 'Example')))
        if found != entry['names'] or not set(entry['excluded']) <= set(found):
            raise SystemExit(f'Test inventory changed in {package}; classify it before running CI.')


def main():
    verify_inventory()
    for package, entry in inventory['packages'].items():
        omitted = list(entry['excluded'])
        if package == 'goodsentry/internal/config':
            omitted.append('TestDeploymentConfiguration')
        command = ['go', 'test', '-race', '-count=1', '-tags=integration']
        if omitted:
            command += ['-skip', '^(' + '|'.join(map(re.escape, omitted)) + ')$']
        run(*command, package)
        if package == 'goodsentry/internal/config':
            skip = '^TestDeploymentConfiguration$/(' + '|'.join(inventory['configExcludedSubtests']) + ')$'
            run('go', 'test', '-race', '-count=1', '-tags=integration', '-run', '^TestDeploymentConfiguration$', '-skip', skip, package)


if __name__ == '__main__':
    try:
        main()
    except subprocess.CalledProcessError as error:
        sys.exit(error.returncode)
