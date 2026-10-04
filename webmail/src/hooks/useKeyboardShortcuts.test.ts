import { it, expect, vi } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { useKeyboardShortcuts } from './useKeyboardShortcuts'
const {navigate}=vi.hoisted(()=>({navigate:vi.fn()}))
vi.mock('react-router-dom',()=>({useNavigate:()=>navigate}))
it('F4736 ignores shortcuts while typing editable content', async()=>{
 vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT',true);navigate.mockClear()
 function Probe(){useKeyboardShortcuts();return null}
 const container=document.createElement('div');document.body.append(container);const root=createRoot(container)
 try {
  await act(async()=>root.render(createElement(Probe)))
  document.body.dispatchEvent(new KeyboardEvent('keydown',{key:'/',bubbles:true,cancelable:true}))
  expect(navigate,'CONTROL FAILED').toHaveBeenCalledWith('/search');console.log('CONTROL EXPECTED: outside editor navigates | ACTUAL: navigated')
  navigate.mockClear();const editor=document.createElement('div');editor.setAttribute('contenteditable','true')
  // jsdom lacks the inherited browser isContentEditable property.
  Object.defineProperty(editor,'isContentEditable',{value:true});container.append(editor)
  editor.dispatchEvent(new KeyboardEvent('keydown',{key:'/',bubbles:true,cancelable:true}))
  const actual=navigate.mock.calls.length;console.log('EXPECTED: editor navigation calls=0 | ACTUAL:',actual)
  if(actual!==0){console.log('PROBLEM CONFIRMED');expect(actual).toBe(0)}
  console.log('PROBLEM NOT REPRODUCED')
  const child=document.createElement('span');Object.defineProperty(child,'isContentEditable',{value:true});editor.append(child)
  const event=new KeyboardEvent('keydown',{key:'n',ctrlKey:true,bubbles:true,cancelable:true});child.dispatchEvent(event)
  expect(navigate).not.toHaveBeenCalled();expect(event.defaultPrevented).toBe(false)
  for(const tag of ['input','textarea']){
   const field=document.createElement(tag);container.append(field)
   field.dispatchEvent(new KeyboardEvent('keydown',{key:'/',bubbles:true,cancelable:true}))
   expect(navigate).not.toHaveBeenCalled()
  }
  console.log('FIX VERIFIED')
 } finally {await act(async()=>root.unmount());container.remove();vi.unstubAllGlobals()}
})
