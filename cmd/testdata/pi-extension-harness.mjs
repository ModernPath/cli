// Run the generated extension without Pi, a model, or a ModernPath server.
import assert from 'node:assert/strict';
import childProcess from 'node:child_process';
import { syncBuiltinESMExports } from 'node:module';
import { pathToFileURL } from 'node:url';

const [extension, scenario] = process.argv.slice(2);
const prompt = 'Explain "authentication"\nUnicode: café 日本語; $(never-execute)';
const context = 'Retrieved context\nwith Unicode: café';
const envelope = JSON.stringify({ hookSpecificOutput: { additionalContext: context } });
const calls = [];
const spawnSync = childProcess.spawnSync;
childProcess.spawnSync = (command, args, options) => {
  calls.push({ command, args, options });
  if (scenario === 'success') {
    // Exercise real child stdin as well as the adapter's argument contract.
    const stub = `const assert = require('node:assert/strict');
      const payload = JSON.parse(require('node:fs').readFileSync(0, 'utf8'));
      assert.deepEqual(payload, ${JSON.stringify({ prompt })});
      process.stdout.write(${JSON.stringify(envelope)});`;
    return spawnSync(process.execPath, ['-e', stub], options);
  }
  if (scenario === 'throws') throw new Error('spawn failed');
  const result = { status: 0, stdout: envelope };
  if (scenario === 'empty') result.stdout = '{}';
  if (scenario === 'malformed') result.stdout = '{';
  if (scenario === 'nonzero') result.status = 1;
  if (scenario === 'null-status') result.status = null;
  if (scenario === 'spawn-error' || scenario === 'timeout') {
    result.error = Object.assign(new Error('child failed'), {
      code: scenario === 'timeout' ? 'ETIMEDOUT' : 'ENOENT',
    });
  }
  return result;
};
syncBuiltinESMExports();
let handler;
const { default: register } = await import(pathToFileURL(extension).href);
register({ on(event, callback) {
  assert.equal(event, 'before_agent_start');
  handler = callback;
} });
assert.equal(typeof handler, 'function');
const event = scenario === 'custom' ? { messageType: 'custom' } : { messageType: 'user', prompt };
const output = await handler(event);
if (scenario === 'custom') {
  assert.equal(output, undefined, 'custom event without a prompt must not inject context');
  assert.equal(calls.length, 0, 'custom event without a prompt must not query context');
} else {
  assert.equal(calls.length, 1);
  const { command, args, options } = calls[0];
  assert.equal(command, 'modernpath');
  assert.deepEqual(args, ['context', '--hook', 'UserPromptSubmit']);
  if (scenario === 'success') {
    assert.deepEqual(output, { message: { customType: 'modernpath', content: context, display: false } });
  } else {
    assert.equal(output, undefined, `${scenario} must not inject a message, even with valid context stdout`);
  }
  assert.deepEqual(JSON.parse(options.input ?? '{}'), { prompt }, 'child stdin must contain the exact Pi prompt');
  assert.notEqual(options.shell, true);
  assert.equal(options.timeout, 30000);
  assert.equal(options.encoding, 'utf8');

}
