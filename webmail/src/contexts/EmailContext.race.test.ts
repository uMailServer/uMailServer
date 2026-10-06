import { it, expect, vi } from 'vitest'
import { act, createElement, useEffect } from 'react'
import { createRoot } from 'react-dom/client'
import { AuthProvider, useAuth } from './AuthContext'
import { EmailProvider, useEmail } from './EmailContext'
import api from '../utils/api'
it('F4735 older folder completion cannot replace current contents', async () => {
 vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT',true)
 const requests: {endpoint:string,resolve:(value:{emails:unknown[]})=>void,reject:(error:Error)=>void}[]=[]
 vi.spyOn(api,'get').mockImplementation((endpoint) => new Promise((resolve,reject)=>requests.push({endpoint,resolve,reject})) as never)
 vi.spyOn(api,'post').mockImplementation(() => Promise.resolve({ ok: true, status: 200, headers: { get: () => 'application/json' }, json: async () => ({}) }) as never)
 let current!:ReturnType<typeof useEmail>
 function Probe(){current=useEmail();return null}
 // The mail load is auth-gated (F-email-auth): log in so the gated mount
 // effect fires and requests[0] exists, exactly as in production.
 // Depend on `login` (stable useCallback) — never the context object identity.
 function LoginProbe(){ const { login } = useAuth(); useEffect(() => { void login('demo@localhost','x') }, [login]); return null }
 const container=document.createElement('div');document.body.append(container);const root=createRoot(container)
 const email=(id:string,folder:string)=>({id,folder,read:false})
 try {
  await act(async()=>root.render(createElement(AuthProvider,null,createElement(LoginProbe),createElement(EmailProvider,{children:createElement(Probe)}))))
  await act(async()=>{ await new Promise(r=>setImmediate(r)) })
  await act(async()=>requests[0].resolve({emails:[email('inbox-control','inbox')]}))
  expect(current.emails[0]?.id,'CONTROL FAILED').toBe('inbox-control')
  console.log('CONTROL EXPECTED: initial inbox contents | ACTUAL:',current.emails[0]?.id)
  act(()=>current.changeFolder('Sent'));act(()=>current.changeFolder('Trash'))
  await act(async()=>requests[2].resolve({emails:[email('trash-current','trash')]}))
  expect(current.currentFolder).toBe('Trash');expect(current.emails[0]?.id).toBe('trash-current')
  await act(async()=>requests[1].resolve({emails:[email('sent-stale','sent')]}))
  const actual=current.emails[0]?.id;console.log('EXPECTED: trash-current | ACTUAL:',actual)
  if(actual!=='trash-current'){console.log('PROBLEM CONFIRMED');expect(actual).toBe('trash-current')}
  console.log('PROBLEM NOT REPRODUCED')
  act(()=>current.changeFolder('Sent'));act(()=>current.changeFolder('Inbox'))
  await act(async()=>requests[4].resolve({emails:[email('inbox-latest','inbox')]}))
  await act(async()=>requests[3].reject(new Error('stale failure')))
  expect(current.emails[0]?.id).toBe('inbox-latest')
  act(()=>current.changeFolder('Sent'));act(()=>current.changeFolder('Trash'))
  await act(async()=>requests[5].resolve({emails:[email('sent-early','sent')]}))
  expect(current.loading).toBe(true)
  await act(async()=>requests[6].resolve({emails:[]}))
  expect(current.loading).toBe(false);expect(current.emails).toEqual([])
  console.log('FIX VERIFIED')
 } finally {await act(async()=>root.unmount());container.remove();vi.restoreAllMocks();vi.unstubAllGlobals()}
})
