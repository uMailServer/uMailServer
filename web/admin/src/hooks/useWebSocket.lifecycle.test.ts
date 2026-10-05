import { it, expect, vi } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { useWebSocket } from './useWebSocket'
class FakeAuditSocket {
 static OPEN=1
 static instances:FakeAuditSocket[]=[]
 readyState=FakeAuditSocket.OPEN
 onopen:((event:Event)=>void)|null=null
 onclose:((event:CloseEvent)=>void)|null=null
 onmessage:((event:MessageEvent)=>void)|null=null
 onerror:((event:Event)=>void)|null=null
 close=vi.fn()
 send=vi.fn()
 constructor(public url:string){FakeAuditSocket.instances.push(this)}
}
it('F4737 explicit disconnect suppresses delayed reconnect', async()=>{
 vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT',true);vi.stubGlobal('WebSocket',FakeAuditSocket);vi.useFakeTimers();FakeAuditSocket.instances=[]
 const metrics=vi.fn();let current!:ReturnType<typeof useWebSocket>
 function Probe(){current=useWebSocket({reconnectInterval:10,onMetrics:metrics});return null}
 const container=document.createElement('div');document.body.append(container);const root=createRoot(container)
 try {
  await act(async()=>root.render(createElement(Probe)))
  const original=FakeAuditSocket.instances[0];act(()=>original.onopen?.(new Event('open')))
  expect(current.isConnected,'CONTROL FAILED').toBe(true);console.log('CONTROL EXPECTED: live socket connects | ACTUAL: connected')
  const delayedClose=original.onclose
  act(()=>current.disconnect())
  expect(current.isConnected).toBe(false)
  act(()=>delayedClose?.(new CloseEvent('close'))) // Release a queued close after disconnect.
  act(()=>vi.advanceTimersByTime(10))
  const actual=FakeAuditSocket.instances.length;console.log('EXPECTED: one retired socket, no reconnect | ACTUAL:',actual)
  if(actual!==1){console.log('PROBLEM CONFIRMED');expect(actual).toBe(1)}
  console.log('PROBLEM NOT REPRODUCED')
  act(()=>original.onopen?.(new Event('open')));expect(current.isConnected).toBe(false)
  act(()=>original.onmessage?.(new MessageEvent('message',{data:JSON.stringify({type:'metrics',data:{fixture:true},timestamp:0})})))
  expect(metrics).not.toHaveBeenCalled();expect(current.lastMessage).toBeNull()
  act(()=>current.connect());const replacement=FakeAuditSocket.instances[1]
  act(()=>replacement.onopen?.(new Event('open')));expect(current.isConnected).toBe(true)
  act(()=>delayedClose?.(new CloseEvent('close')));expect(current.isConnected).toBe(true)
  act(()=>current.sendMessage({fixture:'current'}));expect(replacement.send).toHaveBeenCalledOnce();expect(original.send).not.toHaveBeenCalled()
  act(()=>replacement.onclose?.(new CloseEvent('close')));act(()=>vi.advanceTimersByTime(10))
  expect(FakeAuditSocket.instances).toHaveLength(3)
  console.log('FIX VERIFIED')
 } finally {await act(async()=>root.unmount());container.remove();vi.useRealTimers();vi.unstubAllGlobals()}
})
