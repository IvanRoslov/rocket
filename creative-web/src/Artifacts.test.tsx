import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { beforeAll, afterAll, expect, it } from 'vitest'
import { Artifacts, Media } from './Artifacts'
const artifact={id:2,run_id:7,stage_id:4,name:'Opening scene',kind:'result' as const,current_version_id:12}
const versions=[11,12].map((id,i)=>({id,artifact_id:2,version_number:i+1,filename:'clip.mp4',mime:'video/mp4',size:100,content_url:`/v1/creative/artifact-versions/${id}/content`,download_url:`/v1/creative/artifact-versions/${id}/content?download=1`}))
const server=setupServer(http.get('/v1/creative/runs/7/artifacts',()=>HttpResponse.json([artifact])),http.get('/v1/creative/artifacts/2',()=>HttpResponse.json({artifact,versions})))
beforeAll(()=>server.listen({onUnhandledRequest:'error'}));afterAll(()=>server.close())
it('opens the submitted immutable version and permits inspecting retained history',async()=>{
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}})
 render(<QueryClientProvider client={client}><Artifacts scope={{run_id:7,stage_id:4}} reviewIDs={[11]} onShown={()=>{}}/></QueryClientProvider>)
 expect(await screen.findByRole('link',{name:'Download clip.mp4'})).toHaveAttribute('href','/v1/creative/artifact-versions/11/content?download=1')
 await userEvent.selectOptions(screen.getByLabelText('Opening scene version'),'12')
 expect(screen.getByRole('link',{name:'Download clip.mp4'})).toHaveAttribute('href','/v1/creative/artifact-versions/12/content?download=1')
 expect(screen.getByText(/Current version/)).toBeInTheDocument()
})
it('keeps active document formats downloadable without embedding them',()=>{
 render(<QueryClientProvider client={new QueryClient()}><Media version={{...versions[0],mime:'image/svg+xml',filename:'unsafe.svg'}}/></QueryClientProvider>)
 expect(screen.getByRole('link',{name:'Download unsafe.svg'})).toBeInTheDocument()
 expect(screen.queryByRole('img')).not.toBeInTheDocument()
})
