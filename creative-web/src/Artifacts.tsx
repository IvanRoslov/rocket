import { useEffect, useState } from 'react'
import { useQueries, useQuery } from '@tanstack/react-query'
import { api, base, contextPath, uploadSource, useCommand } from './api'
import type { Artifact, ArtifactDetail, ArtifactVersion, Scope, ShownVersion } from './types'
import { Empty, ErrorNotice } from './ui'
export function Media({version}: {version:ArtifactVersion}) {
 const [failed,setFailed]=useState(false)
 const textType=/^(text\/(plain|markdown|csv)|application\/json)(;|$)/.test(version.mime)
 const text=useQuery({queryKey:['creative','text',version.id],enabled:textType && version.size<=1024*1024,queryFn:async()=>{
  const res=await fetch(version.content_url); if(!res.ok) throw new Error(`Could not load text (${res.status}). Download the file instead.`); return res.text()
 },refetchInterval:false})
 const image=/^image\/(png|jpeg|gif|webp|avif|bmp)(;|$)/.test(version.mime)
 return <div className="media">{!failed && (image?<img src={version.content_url} alt={version.filename} onError={()=>setFailed(true)}/>:version.mime.startsWith('video/')?<video controls preload="metadata" src={version.content_url} onError={()=>setFailed(true)}/>:version.mime.startsWith('audio/')?<audio controls preload="metadata" src={version.content_url} onError={()=>setFailed(true)}/>:textType && version.size<=1024*1024?<><ErrorNotice error={text.error}/><pre>{text.data ?? 'Loading text…'}</pre></>:null)}{failed && <p>This browser could not display the file. Download it to view it.</p>}<a href={version.download_url} download>Download {version.filename}</a></div>
}
export function Artifacts({scope,reviewIDs=[],onShown}: {scope:Scope;reviewIDs?:number[];onShown:(shown:ShownVersion[],ready:boolean)=>void}) {
 const list=useQuery({queryKey:['creative','artifacts',scope],queryFn:()=>api.get<Artifact[]>(contextPath(scope,'artifacts'))})
 const details=useQueries({queries:(list.data ?? []).map(a=>({queryKey:['creative','artifact',a.id],queryFn:()=>api.get<ArtifactDetail>(`${base}/artifacts/${a.id}`)}))})
 const [selected,setSelected]=useState<Record<number,number>>({})
 const [name,setName]=useState(''),[file,setFile]=useState<File>()
 const command=useCommand()
 const shown:ShownVersion[]=details.flatMap(q=>{
  if(!q.data)return []
  const {artifact,versions}=q.data
  const id=selected[artifact.id] ?? versions.find(v=>reviewIDs.includes(v.id))?.id ?? artifact.current_version_id
  const version=versions.find(v=>v.id===id)
  return version?[{artifact,version}]:[]
 })
 const ready=list.isSuccess && details.every(q=>q.isSuccess) && shown.length===(list.data?.length ?? 0)
 const signature=JSON.stringify({shown,ready})
 useEffect(()=>{onShown(shown,ready)},[signature,onShown])
 return <section className="artifacts"><div className="heading"><h2>Sources & results</h2><span>{list.data?.length ?? 0} artifacts</span></div><ErrorNotice error={list.error}/>{list.isPending && <p>Loading artifacts…</p>}{list.data?.length===0 && <Empty>No artifacts here yet. Upload sources or ask the orchestrator to publish results.</Empty>}{details.map((q,index)=>{
  if(q.error)return <ErrorNotice key={index} error={q.error}/>
  if(!q.data)return <p key={index}>Loading artifact history…</p>
  const {artifact,versions}=q.data,entry=shown.find(s=>s.artifact.id===artifact.id)
  if(!entry)return <p key={artifact.id}>Artifact version is unavailable.</p>
  const version=entry.version,current=artifact.current_version_id===version.id
  return <article className="artifact-card" key={artifact.id}><div className="heading"><h3>{artifact.name}</h3><span className="chip">{artifact.kind==='source'?'Source':'Result'}</span></div><label>{artifact.name} version<select value={version.id} onChange={e=>setSelected({...selected,[artifact.id]:Number(e.target.value)})}>{[...versions].reverse().map(v=><option value={v.id} key={v.id}>Version {v.version_number}{v.id===artifact.current_version_id?' · current':''}{reviewIDs.includes(v.id)?' · submitted for review':''}</option>)}</select></label><div className="row"><span className="muted">{current?'Current version':'Retained version'} · {new Intl.NumberFormat('en',{style:'unit',unit:'megabyte',maximumFractionDigits:2}).format(version.size/1e6)}</span>{!current && <button disabled={command.isPending} onClick={()=>command.mutate(()=>api.put(`${base}/artifacts/${artifact.id}/current`,{version_id:version.id}))}>Make current</button>}</div><Media key={version.id} version={version}/></article>
 })}<form className="upload" onSubmit={e=>{e.preventDefault();if(file)command.mutate(()=>uploadSource(scope,file,name || file.name),{onSuccess:()=>{setFile(undefined);setName('')}})}}><h3>Upload source</h3><label>Source name<input value={name} onChange={e=>setName(e.target.value)} placeholder="Optional — defaults to filename"/></label><label>Source file<input type="file" key={file?'selected':'empty'} onChange={e=>setFile(e.target.files?.[0])}/></label><p className="muted">Files up to 2 GiB. Uploads belong to the selected work.</p><button disabled={!file || command.isPending}>{command.isPending?'Uploading…':'Upload source'}</button></form><ErrorNotice error={command.error}/></section>
}
