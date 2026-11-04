import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { afterAll, afterEach, beforeAll, expect, it } from 'vitest'
import { App } from './App'
const created:unknown[]=[]
const server=setupServer(
 http.get('/v1/projects',()=>HttpResponse.json([{id:'film',name:'Film'},{id:'other',name:'Other'}])),
 http.get('/v1/creative/pipelines',({request})=>HttpResponse.json(new URL(request.url).searchParams.get('project_id')==='film'?[{id:3,project_id:'film',name:'Film process',stages:[]}]:[])),
 http.get('/v1/creative/runs',()=>HttpResponse.json([])),
 http.post('/v1/creative/runs',async({request})=>{const body=await request.json();created.push(body);return HttpResponse.json({id:7,...body as object,pipeline_name:'Film process',stages:[],status:'pending',orchestrator_session_id:''})}),
 http.get('/v1/creative/runs/7',()=>HttpResponse.json({id:7,project_id:'film',name:'Opening',brief:'A short film',pipeline_name:'Film process',stages:[],status:'pending',orchestrator_session_id:''})),
 http.get('/v1/creative/runs/7/artifacts',()=>HttpResponse.json([])),
)
beforeAll(()=>server.listen({onUnhandledRequest:'error'}));afterEach(()=>{window.history.replaceState({},'', '/creative/');server.resetHandlers()});afterAll(()=>server.close())
function open(){render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})}><App/></QueryClientProvider>)}
it('creates work with the chosen pipeline and brief, then clears run scope on project switch',async()=>{
 open();const user=userEvent.setup()
 await screen.findByRole('option',{name:'Film'})
 await user.click(screen.getByRole('button',{name:'New work'}))
 await user.type(screen.getByLabelText('Work name'),'Opening')
 await user.type(screen.getByLabelText('Creative brief'),'A short film')
 await user.selectOptions(screen.getByLabelText('Pipeline'),'3')
 await user.click(screen.getByRole('button',{name:'Create work'}))
 await waitFor(()=>expect(created).toEqual([{project_id:'film',pipeline_id:3,name:'Opening',brief:'A short film'}]))
 expect(await screen.findByRole('heading',{name:'Opening'})).toBeInTheDocument()
 await user.selectOptions(screen.getByLabelText('Project'),'other')
 expect(screen.queryByRole('heading',{name:'Opening'})).not.toBeInTheDocument()
 expect(window.location.search).toBe('?project=other')
})
