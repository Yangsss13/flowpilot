import { AlertTriangle, Ban, BookOpen, CheckCircle2, ChevronLeft, ChevronRight, Clock3, Database, Eye, FileStack, FileText, Film, LoaderCircle, RefreshCw, RotateCcw, Search, ShieldCheck, Trash2, UploadCloud, X } from 'lucide-react'
import { DragEvent, FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ApiError, api } from '../api'
import { EmptyState, ErrorState, PageHeader, Panel } from '../components'
import type { DocumentStatus, IngestionJob, IngestionStage, KnowledgeCapability, KnowledgeDocument, KnowledgeDocumentDetail, KnowledgeUploadResponse, SearchResult } from '../types'

const fallbackFormats = ['.txt', '.md']
const fallbackLimits: Record<string, number> = { '.txt': 1 << 20, '.md': 1 << 20 }
const mediaFormats = new Set(['.mp3', '.wav', '.m4a', '.mp4', '.mov', '.webm'])
const pageSize = 8

const stageLabels: Record<IngestionStage, string> = {
  upload: '上传入库', probe: '媒体探测', extract_audio: '提取音轨', transcribe: '语音转写',
  keyframes: '抽取关键帧', ocr: '画面 OCR', merge: '合并时间轴', parse: '解析文档',
  chunk: '结构化切块', embedding: '生成向量', indexing: '写入索引',
}

const documentStatusLabels: Record<DocumentStatus, string> = {
  Queued: '等待处理', Processing: '处理中', Ready: '可检索', Failed: '处理失败', Deleting: '正在清理',
}

function extensionOf(filename: string) {
  const index = filename.lastIndexOf('.')
  return index >= 0 ? filename.slice(index).toLowerCase() : ''
}

function formatBytes(bytes: number) {
  if (bytes >= 1 << 30) return `${(bytes / (1 << 30)).toFixed(1)} GiB`
  if (bytes >= 1 << 20) return `${(bytes / (1 << 20)).toFixed(bytes % (1 << 20) ? 1 : 0)} MiB`
  if (bytes >= 1 << 10) return `${(bytes / (1 << 10)).toFixed(1)} KiB`
  return `${bytes} B`
}

function formatDate(value: string) {
  return new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }).format(new Date(value))
}

function formatTimestamp(milliseconds: number) {
  const totalSeconds = Math.max(0, Math.floor(milliseconds / 1000))
  const hours = Math.floor(totalSeconds / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  return [hours, minutes, seconds].map(value => String(value).padStart(2, '0')).join(':')
}

function sourceReferences(result: SearchResult) {
  const references: string[] = []
  if (result.section) references.push(result.section)
  if (result.page) references.push(`第 ${result.page} 页`)
  if (result.slide) references.push(`第 ${result.slide} 张幻灯片`)
  const start = result.start_time || (result.start_ms != null ? formatTimestamp(result.start_ms) : '')
  const end = result.end_time || (result.end_ms != null ? formatTimestamp(result.end_ms) : '')
  if (start || end) references.push(end && end !== start ? `${start || '00:00:00'} – ${end}` : start || end)
  return references
}

export default function Knowledge({ enabled, capability }: { enabled?: boolean; capability?: KnowledgeCapability }) {
  const inputRef = useRef<HTMLInputElement>(null)
  const [file, setFile] = useState<File | null>(null)
  const [dragging, setDragging] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [uploadResult, setUploadResult] = useState<KnowledgeUploadResponse | null>(null)
  const [job, setJob] = useState<IngestionJob | null>(null)
  const [jobError, setJobError] = useState<string | null>(null)
  const [canceling, setCanceling] = useState(false)
  const [retrying, setRetrying] = useState(false)
  const [uploadError, setUploadError] = useState<string | null>(null)
  const [documents, setDocuments] = useState<KnowledgeDocument[]>([])
  const [documentTotal, setDocumentTotal] = useState(0)
  const [documentPage, setDocumentPage] = useState(1)
  const [documentsLoading, setDocumentsLoading] = useState(true)
  const [documentsError, setDocumentsError] = useState<string | null>(null)
  const [selectedDetail, setSelectedDetail] = useState<KnowledgeDocumentDetail | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [deleteConfirmID, setDeleteConfirmID] = useState<number | null>(null)
  const [deletingIDs, setDeletingIDs] = useState<Set<number>>(new Set())
  const [query, setQuery] = useState('')
  const [topK, setTopK] = useState(5)
  const [searching, setSearching] = useState(false)
  const [results, setResults] = useState<SearchResult[] | null>(null)
  const [searchError, setSearchError] = useState<string | null>(null)

  const supportedFormats = capability?.supported_formats ?? fallbackFormats
  const limits = capability?.max_bytes_by_format ?? fallbackLimits
  const accept = supportedFormats.join(',')
  const hasMedia = capability?.media_ingestion === true && supportedFormats.some(format => mediaFormats.has(format))
  const totalPages = Math.max(1, Math.ceil(documentTotal / pageSize))

  const loadDocuments = useCallback(async () => {
    setDocumentsLoading(true); setDocumentsError(null)
    try {
      const response = await api.listKnowledgeDocuments({ page: documentPage, pageSize })
      setDocuments(response.items); setDocumentTotal(response.total)
      if (response.items.length === 0 && documentPage > 1) setDocumentPage(page => page - 1)
    } catch (error) { setDocumentsError((error as ApiError).message) }
    finally { setDocumentsLoading(false) }
  }, [documentPage])

  useEffect(() => { const timer = window.setTimeout(() => void loadDocuments(), 0); return () => window.clearTimeout(timer) }, [loadDocuments])

  useEffect(() => {
    if (!uploadResult?.job_id) return
    let active = true
    let timer: number | undefined
    const poll = async () => {
      try {
        const next = await api.getKnowledgeJob(uploadResult.job_id)
        if (!active) return
        setJob(next); setJobError(null)
        if (next.status === 'Queued' || next.status === 'Running') timer = window.setTimeout(() => void poll(), 1500)
        else void loadDocuments()
      } catch (error) {
        if (!active) return
        setJobError((error as ApiError).message)
        timer = window.setTimeout(() => void poll(), 2500)
      }
    }
    void poll()
    return () => { active = false; if (timer) window.clearTimeout(timer) }
  }, [uploadResult?.job_id, loadDocuments])

  useEffect(() => {
    if (deletingIDs.size === 0) return
    const timer = window.setInterval(() => {
      void Promise.all([...deletingIDs].map(async id => {
        try { await api.getKnowledgeDocument(id) }
        catch (error) {
          if ((error as ApiError).kind !== 'not-found') return
          setDeletingIDs(current => { const next = new Set(current); next.delete(id); return next })
          setDocuments(current => current.filter(document => document.id !== id))
          setDocumentTotal(current => Math.max(0, current - 1))
        }
      }))
    }, 2000)
    return () => window.clearInterval(timer)
  }, [deletingIDs])

  function choose(next: File | undefined) {
    setUploadError(null); setUploadResult(null); setJob(null); setJobError(null)
    if (!next) return
    const extension = extensionOf(next.name)
    if (!supportedFormats.includes(extension)) return setUploadError(`不支持 ${extension || '无扩展名'} 文件。当前可用：${supportedFormats.join('、')}`)
    const maximum = limits[extension]
    if (!maximum || next.size > maximum) return setUploadError(`${extension} 文件上限为 ${formatBytes(maximum || 0)}。`)
    if (next.size === 0) return setUploadError('文件不能为空。')
    setFile(next)
  }

  function drop(event: DragEvent) { event.preventDefault(); setDragging(false); choose(event.dataTransfer.files[0]) }

  async function upload() {
    if (!file) return
    setUploading(true); setUploadError(null); setJob(null); setJobError(null)
    try {
      const response = await api.importDocument(file)
      setUploadResult(response); setFile(null)
      if (inputRef.current) inputRef.current.value = ''
    } catch (error) {
      const apiError = error as ApiError
      setUploadError(apiError.kind === 'not-found' ? '知识摄取接口尚未启用，请检查后端配置。' : apiError.message)
    } finally { setUploading(false) }
  }

  async function cancelJob() {
    if (!job) return
    setCanceling(true); setJobError(null)
    try { setJob(await api.cancelKnowledgeJob(job.id)) }
    catch (error) { setJobError((error as ApiError).message) }
    finally { setCanceling(false) }
  }

  async function retryJob() {
    if (!job) return
    setRetrying(true); setJobError(null)
    try { setJob(await api.retryKnowledgeJob(job.id)) }
    catch (error) { setJobError((error as ApiError).message) }
    finally { setRetrying(false) }
  }

  async function viewDocument(id: number) {
    setDetailLoading(true); setDocumentsError(null)
    try { setSelectedDetail(await api.getKnowledgeDocument(id)) }
    catch (error) { setDocumentsError((error as ApiError).message) }
    finally { setDetailLoading(false) }
  }

  async function deleteDocument(id: number) {
    setDocumentsError(null)
    try {
      await api.deleteKnowledgeDocument(id)
      setDeleteConfirmID(null)
      setDeletingIDs(current => new Set(current).add(id))
      setDocuments(current => current.map(document => document.id === id ? { ...document, status: 'Deleting' } : document))
      if (selectedDetail?.document.id === id) setSelectedDetail(null)
    } catch (error) { setDocumentsError((error as ApiError).message) }
  }

  async function search(event: FormEvent) {
    event.preventDefault(); setSearching(true); setSearchError(null); setResults(null)
    try { setResults((await api.searchKnowledge(query.trim(), topK)).results) }
    catch (error) { setSearchError((error as ApiError).message) }
    finally { setSearching(false) }
  }

  const formatSummary = useMemo(() => supportedFormats.map(format => `${format} ${formatBytes(limits[format] ?? 0)}`).join(' · '), [limits, supportedFormats])

  if (enabled === false) return <><PageHeader eyebrow="KNOWLEDGE LAYER" title="知识库" description="导入资料并通过可追踪的异步任务构建检索知识。" /><ErrorState message="当前后端未启用知识库，请配置 Embedding 模型后重启服务。" /></>

  const jobActive = job?.status === 'Queued' || job?.status === 'Running'
  const cancelRequested = Boolean(job?.cancel_requested_at)

  return <>
    <PageHeader eyebrow="KNOWLEDGE LAYER" title="知识库" description="上传文档、音频或视频，由后端异步解析、切块并建立可引用的知识索引。" />

    <div className="knowledge-grid knowledge-ingestion-grid">
      <Panel><div className="panel-heading"><div><span className="step-label">01</span><h2>上传资料</h2></div>{hasMedia ? <Film size={19} /> : <UploadCloud size={19} />}</div>
        <button type="button" className={`dropzone ${dragging ? 'is-dragging' : ''}`} onClick={() => inputRef.current?.click()} onDragOver={event => { event.preventDefault(); setDragging(true) }} onDragLeave={() => setDragging(false)} onDrop={drop}>
          <span className="upload-icon">{hasMedia ? <Film size={25} /> : <UploadCloud size={25} />}</span><strong>拖拽文件到这里</strong><p>或点击从本地选择文件</p><small>{supportedFormats.join(' · ')}</small>
        </button>
        <input ref={inputRef} hidden type="file" accept={accept} onChange={event => choose(event.target.files?.[0])} />
        {file && <div className="selected-file"><FileText size={19} /><div><strong>{file.name}</strong><small>{formatBytes(file.size)} · 上传后进入异步队列</small></div><button aria-label="移除已选文件" onClick={() => setFile(null)}><X size={16} /></button></div>}
        {uploadError && <div className="inline-error"><AlertTriangle size={16} />{uploadError}</div>}
        <button className="button button-primary button-wide" onClick={upload} disabled={!file || uploading}>{uploading ? <><LoaderCircle size={17} className="spin" />正在上传…</> : <><UploadCloud size={17} />上传并创建摄取任务</>}</button>
        <div className="process-note"><ShieldCheck size={16} /><span>{formatSummary}{capability?.max_media_duration_seconds ? ` · 媒体最长 ${Math.round(capability.max_media_duration_seconds / 3600)} 小时` : ''}</span></div>
      </Panel>

      <Panel className="ingestion-panel"><div className="panel-heading"><div><span className="step-label">02</span><h2>摄取进度</h2></div><Database size={19} /></div>
        {!uploadResult ? <EmptyState title="等待上传" description="收到 202 只表示已排队；完成状态以摄取 Job 为准。" /> : !job ? <div className="ingestion-loading"><LoaderCircle className="spin" size={25} /><strong>已进入摄取队列</strong><span>Job #{uploadResult.job_id} · 正在读取处理进度</span></div> : <div className="ingestion-job">
          <div className="ingestion-job-head"><div><span className={`knowledge-status knowledge-status-${job.status.toLowerCase()}`}>{job.status === 'Success' && <CheckCircle2 size={14} />}{job.status === 'Failed' && <AlertTriangle size={14} />}{job.status === 'Canceled' && <Ban size={14} />}{jobActive && <Clock3 size={14} />}{job.status}</span><strong>Job #{job.id}</strong></div><span>{job.progress}%</span></div>
          <div className="progress-track"><i style={{ width: `${Math.max(0, Math.min(100, job.progress))}%` }} /></div>
          <div className="ingestion-stage"><span>当前阶段</span><strong>{stageLabels[job.stage]}</strong><small>{job.stage}</small></div>
          {uploadResult.deduplicated && <div className="ingestion-notice">检测到相同内容，已复用现有文档与摄取任务。</div>}
          {job.status === 'Success' && <div className="import-success"><CheckCircle2 size={20} /><div><strong>知识导入完成</strong><span>文档 #{job.document_id} 已可用于检索</span></div></div>}
          {job.safe_error_message && <div className={`job-safe-error ${job.status === 'Canceled' ? 'job-canceled' : ''}`}><strong>{job.safe_error_code || job.status}</strong><span>{job.safe_error_message}</span></div>}
          {jobError && <div className="inline-error"><AlertTriangle size={16} />{jobError}</div>}
          <div className="ingestion-actions">
            {jobActive && <button className="button button-secondary" onClick={cancelJob} disabled={canceling || cancelRequested}>{canceling || cancelRequested ? <LoaderCircle className="spin" size={15} /> : <Ban size={15} />}{cancelRequested ? '正在取消…' : '取消任务'}</button>}
            {job.status === 'Failed' && <button className="button button-secondary" onClick={retryJob} disabled={retrying}>{retrying ? <LoaderCircle className="spin" size={15} /> : <RotateCcw size={15} />}重试摄取</button>}
          </div>
        </div>}
      </Panel>
    </div>

    <Panel className="document-library"><div className="panel-heading"><div><span className="eyebrow">DOCUMENT LIBRARY</span><h2>文档列表</h2></div><div className="document-library-actions"><span>{documentTotal} 个文档</span><button className="icon-button" title="刷新文档列表" onClick={() => void loadDocuments()}><RefreshCw size={16} /></button></div></div>
      {documentsError && <div className="inline-error"><AlertTriangle size={16} />{documentsError}</div>}
      {documentsLoading ? <div className="document-loading"><LoaderCircle className="spin" size={22} />正在读取文档</div> : documents.length === 0 ? <EmptyState title="还没有文档" description="上传资料后，摄取状态会显示在这里。" /> : <div className="document-list">{documents.map(document => {
        const deleting = document.status === 'Deleting' || deletingIDs.has(document.id)
        return <article key={document.id} className={`document-card document-${document.status.toLowerCase()}`}><div className="document-kind">{mediaFormats.has(extensionOf(document.filename)) ? <Film size={20} /> : <FileStack size={20} />}</div><div className="document-copy"><div><strong>{document.filename}</strong><span className={`document-status document-status-${document.status.toLowerCase()}`}>{documentStatusLabels[document.status]}</span></div><p>{document.media_type} · {formatBytes(document.size_bytes)} · v{document.current_version || '—'}</p><small>#{document.id} · {formatDate(document.created_at)}</small></div><div className="document-actions"><button className="button button-secondary" onClick={() => void viewDocument(document.id)} disabled={deleting || detailLoading}><Eye size={14} />详情</button>{deleteConfirmID === document.id ? <button className="button delete-confirm" onClick={() => void deleteDocument(document.id)}>确认删除</button> : <button className="icon-button delete-button" title="删除文档" onClick={() => setDeleteConfirmID(document.id)} disabled={deleting}>{deleting ? <LoaderCircle className="spin" size={15} /> : <Trash2 size={15} />}</button>}</div></article>
      })}</div>}
      <div className="document-pagination"><button className="button button-secondary" onClick={() => setDocumentPage(page => Math.max(1, page - 1))} disabled={documentPage <= 1}><ChevronLeft size={15} />上一页</button><span>第 {documentPage} / {totalPages} 页</span><button className="button button-secondary" onClick={() => setDocumentPage(page => Math.min(totalPages, page + 1))} disabled={documentPage >= totalPages}>下一页<ChevronRight size={15} /></button></div>
    </Panel>

    {selectedDetail && <Panel className="document-detail-panel"><div className="panel-heading"><div><span className="eyebrow">DOCUMENT DETAIL</span><h2>{selectedDetail.document.filename}</h2></div><button className="icon-button" title="关闭详情" onClick={() => setSelectedDetail(null)}><X size={16} /></button></div><div className="document-detail-grid"><div><span>文档状态</span><strong>{documentStatusLabels[selectedDetail.document.status]}</strong></div><div><span>当前版本</span><strong>v{selectedDetail.document.current_version || '—'}</strong></div><div><span>Chunk 数量</span><strong>{selectedDetail.current_version?.chunk_count ?? '—'}</strong></div><div><span>解析器</span><strong>{selectedDetail.current_version?.parser_version ?? '—'}</strong></div></div>{selectedDetail.latest_job && <div className="detail-job"><div><span>最近任务</span><strong>#{selectedDetail.latest_job.id} · {selectedDetail.latest_job.status}</strong></div><div><span>阶段</span><strong>{stageLabels[selectedDetail.latest_job.stage]}</strong></div><div><span>进度</span><strong>{selectedDetail.latest_job.progress}%</strong></div>{selectedDetail.latest_job.safe_error_message && <div><span>安全错误</span><strong>{selectedDetail.latest_job.safe_error_message}</strong></div>}</div>}</Panel>}

    <div className="knowledge-search-layout"><Panel><div className="panel-heading"><div><span className="step-label">03</span><h2>检索测试</h2></div><Search size={19} /></div><form onSubmit={search} className="search-form"><label>检索问题<textarea value={query} onChange={event => setQuery(event.target.value)} rows={4} placeholder="例如：视频中在什么时候解释了退款流程？" required /></label><label>TopK <span>{topK} 个结果</span><input type="range" min="1" max="10" value={topK} onChange={event => setTopK(Number(event.target.value))} /></label><button className="button button-primary button-wide" disabled={!query.trim() || searching}>{searching ? <><LoaderCircle size={17} className="spin" />正在检索…</> : <><Search size={17} />测试知识检索</>}</button></form>{searchError && <div className="inline-error"><AlertTriangle size={16} />{searchError}</div>}</Panel>
      <Panel className="results-panel"><div className="panel-heading"><div><span className="eyebrow">SEMANTIC RESULTS</span><h2>检索结果</h2></div>{results && <span className="muted">返回 {results.length} 个片段</span>}</div>{results === null ? <EmptyState title="等待一次检索" description="结果会保留页码、幻灯片、章节和媒体时间轴引用。" /> : results.length === 0 ? <EmptyState title="没有匹配片段" description="尝试换一种问法，或等待资料摄取完成。" /> : <div className="search-results">{results.map((result, index) => { const references = sourceReferences(result); return <article key={`${result.document_id}-${result.version_id ?? 0}-${result.chunk_index}`}><div className="result-rank">{String(index + 1).padStart(2, '0')}</div><div><div className="result-head"><strong><BookOpen size={14} />{result.source}</strong><span>Chunk {result.chunk_index}</span><em>{(result.score * 100).toFixed(1)}%</em></div>{references.length > 0 && <div className="source-reference-list">{references.map(reference => <span key={reference}>{reference}</span>)}</div>}<p>{result.text}</p></div></article> })}</div>}</Panel>
    </div>
  </>
}
