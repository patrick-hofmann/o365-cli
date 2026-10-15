// @vitest-environment node
import { expect, it } from 'vitest'
import { parseMailRequest, parseMailScope } from '../src/contract.js'

const scope = { account: 'pod@example.invalid', folders: ['inbox', 'sentitems', 'rules'], attachments: false }
const request = { toolId: 'o365-mail', argv: ['o365-cli', 'pods', 'read', '--operation', 'messages', '--account', scope.account, '--folder', 'inbox'] }
it('binds read commands to the exact account, folder and attachment permission', () => {
  expect(parseMailRequest(request, scope).read).toEqual({ operation: 'messages', folder: 'inbox' })
  for (const argv of [request.argv.map(value => value === 'inbox' ? 'foreign' : value), request.argv.map(value => value === scope.account ? 'other@example.invalid' : value), [...request.argv, '--cache-dir', '/tmp/stolen'], [...request.argv, '--folder', 'sentitems'], request.argv.map(value => value === 'messages' ? 'send' : value), [...request.argv.map(value => value === 'messages' ? 'attachments' : value), '--message', 'm1']]) {
    expect(() => parseMailRequest({ ...request, argv }, scope)).toThrow()
  }
})
it('requires the exact history filter on message reads and keeps attachment filtering out of CLI arguments', () => {
  const bounded = { ...scope, since: '2026-06-01T00:00:00Z', attachments: true }
  expect(() => parseMailRequest(request, bounded)).toThrow('history')
  expect(parseMailRequest({ ...request, argv: [...request.argv, '--since', bounded.since] }, bounded).read.operation).toBe('messages')
  expect(() => parseMailRequest({ ...request, argv: [...request.argv, '--since', '2026-05-01T00:00:00Z'] }, bounded)).toThrow('history')
  const attachments = [...request.argv.map(value => value === 'messages' ? 'attachments' : value), '--message', 'm1']
  expect(parseMailRequest({ ...request, argv: attachments }, bounded).read.message).toBe('m1')
  expect(() => parseMailRequest({ ...request, argv: [...attachments, '--since', bounded.since] }, bounded)).toThrow('history')
})

it('rejects ambiguous scope before provider access', () => {
  expect(parseMailScope(scope)).toEqual({ ...scope, since: null })
  for (const value of [{ ...scope, account: '*' }, { ...scope, folders: [] }, { ...scope, folders: ['../inbox'] }, { ...scope, attachments: 'yes' }, { ...scope, since: '2026-02-30T00:00:00Z' }, { ...scope, unexpected: true }]) expect(() => parseMailScope(value)).toThrow()
})
