"""Run deploy.sh with offline command stubs: no Docker, Git updates, or network."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

TARGET = '0123456789abcdef0123456789abcdef01234567'
OLD = '790d44b'


class DeployVersionTest(unittest.TestCase):
    def run_deploy(self, responses, domain='setori.invalid'):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            deploy = root / 'deploy'
            deploy.mkdir()
            shutil.copyfile(Path(__file__).resolve().parents[1] / 'deploy.sh', deploy / 'deploy.sh')
            (deploy / '.env').write_text('DOMAIN=' + domain + '\n')
            bindir = root / 'bin'
            bindir.mkdir()
            (root / 'responses.json').write_text(json.dumps(responses))
            commands = {
                'git': '#!/bin/sh\ncase "$*" in *"--short HEAD") echo ' + TARGET[:7] + ';; *"rev-parse HEAD") echo ' + TARGET + ';; *) exit 90;; esac\n',
                'docker': '#!/bin/sh\necho "$*" >> "$DEPLOY_PROBE_DIR/docker.log"\nexit 0\n',
                'sleep': '#!/bin/sh\nexit 0\n',
                'curl': '#!' + shutil.which('python3') + '\n' + '''import json, os, pathlib, sys, time
root = pathlib.Path(os.environ['DEPLOY_PROBE_DIR'])
with (root / 'curl.log').open('a') as output:
    output.write(json.dumps(sys.argv[1:]) + '\\n')
count_file = root / 'count'
count = int(count_file.read_text()) if count_file.exists() else 0
responses = json.loads((root / 'responses.json').read_text())
response = responses[min(count, len(responses) - 1)]
count_file.write_text(str(count + 1))
if isinstance(response, dict):
    time.sleep(response.get('delay', 0))
    if 'exit' in response:
        sys.exit(response['exit'])
    response = response['body']
if response is None:
    sys.exit(22)
print(response)
''',
            }
            for name, content in commands.items():
                path = bindir / name
                path.write_text(content)
                path.chmod(0o755)
            env = dict(os.environ, PATH=str(bindir) + os.pathsep + os.environ['PATH'],
                       DEPLOY_REEXEC='1', DEPLOY_PROBE_DIR=str(root))
            result = subprocess.run(['bash', str(deploy / 'deploy.sh')], env=env,
                                    capture_output=True, text=True, timeout=10)
            calls = [json.loads(line) for line in (root / 'curl.log').read_text().splitlines()] if (root / 'curl.log').exists() else []
            self.assertIn('compose up -d --no-build --remove-orphans', (root / 'docker.log').read_text())
            for call in calls:
                self.assertIn('--max-time', call)
                self.assertEqual(call[call.index('--max-time') + 1], '10')
                self.assertEqual(call[-1], 'https://setori.invalid/api/version')
            return result, calls

    def test_target_and_different_abbreviations(self):
        for commit in (TARGET[:7], TARGET[:12], TARGET):
            with self.subTest(commit=commit):
                result, calls = self.run_deploy([json.dumps({'built_at': '', 'commit': commit}, indent=2)])
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(len(calls), 1)
                self.assertIn(commit, result.stdout)

    def test_waits_for_target_after_old_version(self):
        result, calls = self.run_deploy([json.dumps({'commit': OLD}), json.dumps({'commit': TARGET[:7]})])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(calls), 2)

    def test_timeout_then_delayed_target(self):
        # A curl timeout is retryable, and a response arriving within the
        # deadline still succeeds. The stub does not measure real curl timing.
        result, calls = self.run_deploy([
            {'exit': 28},
            {'delay': 0.1, 'body': json.dumps({'commit': TARGET[:7]})},
        ])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(calls), 2)

    def test_wrong_missing_or_ambiguous_commit_fails(self):
        for response in (json.dumps({'commit': OLD}), json.dumps({'commit': 'dev'}),
                         json.dumps({'commit': TARGET[:6]}), '<html>502</html>', '{}', '',
                         '{"commit":"' + TARGET[:7] + '","commit":"' + OLD + '"}', None):
            with self.subTest(response=response):
                result, calls = self.run_deploy([response])
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(len(calls), 30)
                self.assertIn('対象版', result.stderr)

    def test_duplicate_commit_with_non_hex_value_fails(self):
        for response in ('{"commit":"' + TARGET[:7] + '","commit":"dev"}',
                         '{"commit":"dev","commit":"' + TARGET[:7] + '"}'):
            with self.subTest(response=response):
                result, calls = self.run_deploy([response])
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(len(calls), 30)

    def test_missing_domain_cannot_succeed(self):
        result, calls = self.run_deploy([json.dumps({'commit': TARGET[:7]})], domain='')
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(calls)
        self.assertIn('DOMAIN', result.stderr)


if __name__ == '__main__':
    unittest.main()
