// Run with: node --test src/lib/server/tabs/files-sort.test.mjs (Node 22.18 or newer strips the types by itself).
import test from 'node:test';
import assert from 'node:assert/strict';
import { sortEntries } from './files-sort.ts';

const e = (name, type, size = 0, mtime = 0) => ({ name, type, size, mtime, mode: '0644', mode_str: '-rw-r--r--', owner: 'root', group: 'root' });
const names = (list) => list.map((x) => x.name);
const sample = [e('b.txt', 'file', 10, 5), e('a.txt', 'file', 300, 1), e('zdir', 'dir'), e('adir', 'dir'), e('c.log', 'file', 50, 9)];

test('by name by default, folders first', () => {
  assert.deepEqual(names(sortEntries(sample)), ['adir', 'zdir', 'a.txt', 'b.txt', 'c.log']);
});

test('descending order keeps folders on top', () => {
  assert.deepEqual(names(sortEntries(sample, 'name', 'desc')), ['zdir', 'adir', 'c.log', 'b.txt', 'a.txt']);
});

test('by size and by date', () => {
  assert.deepEqual(names(sortEntries(sample, 'size', 'desc')).slice(2), ['a.txt', 'c.log', 'b.txt']);
  assert.deepEqual(names(sortEntries(sample, 'mtime', 'asc')).slice(2), ['a.txt', 'b.txt', 'c.log']);
});

test('the input is not mutated and null is an empty list', () => {
  const copy = [...sample];
  sortEntries(sample, 'size', 'desc');
  assert.deepEqual(sample, copy);
  assert.deepEqual(sortEntries(null), []);
});
