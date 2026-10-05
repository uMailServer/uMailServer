import { it, expect, vi } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { useApi, useDomains, useAccounts } from './useApi'
it('F4738 successful retry clears stale specialized API error', async()=>{
 vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT',true)
 const failure={ok:false,status:503,json:async()=>({error:'temporary failure'})}
 const success={ok:true,status:200,json:async()=>[{name:'example.com'}]}
 const fetchMock=vi.fn();vi.stubGlobal('fetch',fetchMock)
 let generic!:ReturnType<typeof useApi>;let domains!:ReturnType<typeof useDomains>;let accounts!:ReturnType<typeof useAccounts>
 function Probe(){generic=useApi();domains=useDomains();accounts=useAccounts();return null}
 const container=document.createElement('div');document.body.append(container);const root=createRoot(container)
 try {
  await act(async()=>root.render(createElement(Probe)))
  fetchMock.mockResolvedValueOnce(failure).mockResolvedValueOnce(success)
  await act(async()=>{await generic.execute('/fixture').catch(()=>undefined)})
  await act(async()=>{await generic.execute('/fixture')})
  expect(generic.error,'CONTROL FAILED').toBeNull();console.log('CONTROL EXPECTED: generic retry error=null | ACTUAL:',generic.error)
  fetchMock.mockResolvedValueOnce(failure).mockResolvedValueOnce(success)
  await act(async()=>{await domains.fetchDomains().catch(()=>undefined)})
  expect(domains.error?.message).toBe('temporary failure')
  await act(async()=>{await domains.fetchDomains()})
  const actual=domains.error?.message ?? null;console.log('EXPECTED: recovered domain error=null | ACTUAL:',actual)
  if(actual!==null){console.log('PROBLEM CONFIRMED');expect(actual).toBeNull()}
  console.log('PROBLEM NOT REPRODUCED')
  fetchMock.mockResolvedValueOnce(failure).mockResolvedValueOnce(success)
  await act(async()=>{await accounts.fetchAccounts('example.com').catch(()=>undefined)})
  expect(accounts.error?.message).toBe('temporary failure')
  await act(async()=>{await accounts.fetchAccounts('example.com')})
  expect(accounts.error).toBeNull();expect(accounts.accounts).toHaveLength(1)
  fetchMock.mockResolvedValueOnce(failure).mockResolvedValueOnce({ok:true,status:200,json:async()=>[]})
  await act(async()=>{await domains.fetchDomains().catch(()=>undefined)})
  await act(async()=>{await domains.fetchDomains()})
  expect(domains.error).toBeNull();expect(domains.domains).toEqual([])
  console.log('FIX VERIFIED')
  void accounts
 } finally {await act(async()=>root.unmount());container.remove();vi.unstubAllGlobals()}
})
