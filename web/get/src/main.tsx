import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  Activity,
  Calendar,
  Check,
  ChevronRight,
  Database,
  Download,
  FileText,
  HardDrive,
  Network,
  Link,
  Magnet,
  Pause,
  Play,
  RadioTower,
  Search,
  Trash2,
  X,
  Users
} from "lucide-react";
import "./styles.css";

type TorrentStatus = "unavailable" | "idle" | "loading" | "ready" | "error";
type FileStatus = "idle" | "cached" | "downloading" | "complete";
type DownloadTaskStatus = "downloading" | "paused" | "complete" | "canceled";

type FileItem = {
  path: string;
  bytes: number;
  completedBytes: number;
  cachedBytes: number;
  savePath: string;
  status: FileStatus;
  task?: TaskItem;
};

type TorrentItem = {
  id: string;
  title: string;
  provider: string;
  bytes?: number;
  category?: string;
  date?: string;
  seeders: number;
  peers: number;
  hash?: string;
  magnetUrl?: string;
  status: TorrentStatus;
  downloading: boolean;
  error?: string;
  files?: FileItem[];
};

type AppState = {
  updated: string;
  saveTo: string;
  torrents: TorrentState[];
};

type TaskItem = {
  id: string;
  status: DownloadTaskStatus;
  completedBytes: number;
  bytes: number;
};

type TorrentState = TorrentItem & {
  runtime: RuntimeView;
};

type RuntimeView = {
  status: "pending" | "ready" | "error";
  snapshot?: RuntimeSnapshot;
  error?: string;
};

type TorrentStore = {
  items: TorrentState[];
  error: string;
  inFlightCommands: Set<string>;
  startDownload: (item: TorrentState, path: string) => Promise<void>;
  runDownloadAction: (task: TaskItem, action: DownloadAction) => Promise<void>;
};

type RuntimeSnapshot = {
  id: string;
  updated: string;
  summary: RuntimeSummary;
  peers: RuntimePeer[];
  pieceRuns: RuntimePieceRun[];
  dht: RuntimeDHTServer[];
  events: RuntimeTorrentEvent[];
};

type RuntimeSummary = {
  infoHash?: string;
  name?: string;
  metadataReady: boolean;
  bytesCompleted: number;
  length: number;
  pendingPeers: number;
  activePeers: number;
  connectedSeeders: number;
  halfOpenPeers: number;
  piecesComplete: number;
  numPieces: number;
  chunksReadUseful: number;
  chunksReadWasted: number;
  bytesReadUsefulData: number;
  bytesWrittenData: number;
};

type RuntimePeer = {
  address: string;
  source: string;
  network?: string;
  client?: string;
  downloadRate: number;
  connected: boolean;
};

type RuntimePieceRun = {
  length: number;
  state: string;
  complete: boolean;
  partial: boolean;
  hashing: boolean;
  queuedHash: boolean;
  priority: string;
};

type RuntimeDHTServer = {
  id: string;
  address: string;
  nodes: number;
  goodNodes: number;
  outstandingTransactions: number;
  outboundQueriesAttempted: number;
  successfulOutboundAnnouncePeers: number;
  badNodes: number;
};

type RuntimeTorrentEvent = {
  time: string;
  type: string;
  infoHash?: string;
  peer?: string;
  source?: string;
  network?: string;
  client?: string;
  piece?: number;
  begin?: number;
  length?: number;
  message?: string;
  error?: string;
  dhtQuery?: string;
  dhtNode?: string;
};

type DownloadAction = "pause" | "resume" | "cancel" | "delete";
type BatchAction = "download" | DownloadAction;

class HTTPError extends Error {
  readonly status: number;
  readonly body: string;

  constructor(status: number, body: string) {
    super(body.trim() || `HTTP ${status}`);
    this.name = "HTTPError";
    this.status = status;
    this.body = body;
  }
}

const pollIntervalMs = 1000;
const pieceCellSize = 11;
const pieceCellGap = 3;
const pieceGridHeight = 260;
const pieceGridOverscanRows = 4;

const statusLabels: Record<TorrentStatus, string> = {
  unavailable: "Unavailable",
  idle: "Metadata pending",
  loading: "Metadata pending",
  ready: "Files ready",
  error: "Metadata failed"
};

function App() {
  const { items, error, inFlightCommands, startDownload, runDownloadAction } = useTorrents();
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [filter, setFilter] = useState("");

  const visibleItems = useMemo(() => filterTorrents(items, filter), [items, filter]);

  function toggle(item: TorrentItem) {
    const willOpen = !expanded[item.id];
    setExpanded((current) => ({ ...current, [item.id]: willOpen }));
  }

  return (
    <main className="shell">
      <header className="topbar">
        <div>
          <h1>mvdl get</h1>
          <div className="meta">{items.length} torrents</div>
        </div>
        <label className="searchBox">
          <Search size={16} />
          <input
            value={filter}
            onChange={(event) => setFilter(event.target.value)}
            placeholder="Filter"
          />
        </label>
      </header>

      {error && <div className="banner">{error}</div>}

      <TorrentList
        items={visibleItems}
        inFlightCommands={inFlightCommands}
        expanded={expanded}
        onDownload={startDownload}
        onDownloadAction={runDownloadAction}
        onToggle={toggle}
      />
    </main>
  );
}

function useTorrents(): TorrentStore {
  const [state, setState] = useState<AppState | null>(null);
  const [error, setError] = useState("");
  const [inFlightCommands, setInFlightCommands] = useState<Set<string>>(new Set());
  const inFlightCommandsRef = useRef<Set<string>>(new Set());
  const stateRef = useRef<AppState | null>(null);

  useEffect(() => {
    stateRef.current = state;
  }, [state]);

  const refreshState = useCallback(async () => {
    const next = await getJSON<AppState>(stateEndpoint());
    setState(next);
    return next;
  }, []);

  const showServiceError = useCallback((err: unknown) => {
    setError(serviceErrorMessage(err));
  }, []);

  const startDownload = useCallback(
    async (item: TorrentItem, path: string) => {
      const commandKey = downloadKey(item.id, path);
      const file = (item.files ?? []).find((candidate) => candidate.path === path);
      if (
        inFlightCommandsRef.current.has(commandKey) ||
        file?.status === "complete" ||
        file?.status === "downloading" ||
        file?.task !== undefined
      ) {
        return;
      }
      addInFlightCommand(inFlightCommandsRef.current, setInFlightCommands, commandKey);
      setError("");
      try {
        const next = await postJSON<AppState>(torrentFileDownloadEndpoint(item.id), { path });
        setState(next);
      } catch (err) {
        showServiceError(err);
      } finally {
        clearInFlightCommand(inFlightCommandsRef.current, setInFlightCommands, commandKey);
      }
    },
    [showServiceError]
  );

  const runDownloadAction = useCallback(
    async (task: TaskItem, action: DownloadAction) => {
      const commandKey = taskCommandKey(task.id, action);
      if (inFlightCommandsRef.current.has(commandKey)) {
        return;
      }
      addInFlightCommand(inFlightCommandsRef.current, setInFlightCommands, commandKey);
      setError("");
      try {
        const next = await postJSON<AppState>(downloadActionEndpoint(task.id, action), {});
        setState(next);
      } catch (err) {
        showServiceError(err);
      } finally {
        clearInFlightCommand(inFlightCommandsRef.current, setInFlightCommands, commandKey);
      }
    },
    [showServiceError]
  );

  useEffect(() => {
    void refreshState()
      .catch((err) => setError(serviceErrorMessage(err)));
  }, [refreshState]);

  useEffect(() => {
    let connected = false;
    const source = new EventSource(stateStreamEndpoint());
    source.addEventListener("open", () => {
      connected = true;
      setError("");
    });
    source.addEventListener("state", (event) => {
      connected = true;
      setState(JSON.parse(event.data) as AppState);
    });
    source.addEventListener("error", () => {
      connected = false;
    });

    const interval = window.setInterval(() => {
      if (connected || document.hidden) {
        return;
      }
      const items = stateRef.current?.torrents ?? [];
      const hasActive = items.some(
        (i) => i.status === "loading" || i.downloading || (i.files ?? []).some((file) => file.status === "downloading")
      );
      if (!hasActive) {
        return;
      }
      void refreshState().catch(showServiceError);
    }, pollIntervalMs);
    return () => {
      source.close();
      window.clearInterval(interval);
    };
  }, [refreshState, showServiceError]);

  return {
    items: state?.torrents ?? [],
    error,
    inFlightCommands,
    startDownload,
    runDownloadAction
  };
}

function TorrentList({
  items,
  inFlightCommands,
  expanded,
  onDownload,
  onDownloadAction,
  onToggle
}: {
  items: TorrentState[];
  inFlightCommands: Set<string>;
  expanded: Record<string, boolean>;
  onDownload: (item: TorrentState, path: string) => Promise<void>;
  onDownloadAction: (task: TaskItem, action: DownloadAction) => Promise<void>;
  onToggle: (item: TorrentState) => void;
}) {
  return (
    <section className="torrentList" aria-label="Torrents">
      {items.length === 0 && <div className="emptyBox">No torrents</div>}
      {items.map((item) => (
        <TorrentCard
          key={item.id}
          item={item}
          inFlightCommands={inFlightCommands}
          open={Boolean(expanded[item.id])}
          onDownload={onDownload}
          onDownloadAction={onDownloadAction}
          onToggle={() => onToggle(item)}
        />
      ))}
    </section>
  );
}

const TorrentCard = React.memo(function TorrentCard({
  item,
  inFlightCommands,
  open,
  onDownload,
  onDownloadAction,
  onToggle
}: {
  item: TorrentState;
  inFlightCommands: Set<string>;
  open: boolean;
  onDownload: (item: TorrentState, path: string) => Promise<void>;
  onDownloadAction: (task: TaskItem, action: DownloadAction) => Promise<void>;
  onToggle: () => void;
}) {
  return (
    <article className={`torrentItem ${open ? "open" : ""} ${item.downloading ? "downloading" : ""}`}>
      <div
        className="torrentSummary"
        onClick={onToggle}
        onKeyDown={(event) => handleToggleKeyDown(event, onToggle)}
        role="button"
        tabIndex={0}
      >
        <div className={`disclosure ${open ? "open" : ""}`} aria-hidden="true">
          <ChevronRight size={18} />
        </div>

        <div className="torrentBody">
          <h2>{item.title || "Untitled"}</h2>
          <div className="metaPills">
            <MetaPill icon={<Database size={14} />} value={item.provider || "-"} />
            <MetaPill icon={<HardDrive size={14} />} value={formatBytes(item.bytes)} />
            <MetaPill icon={<Users size={14} />} value={`${item.seeders}/${item.peers}`} />
            <MetaPill icon={<Calendar size={14} />} value={formatDate(item.date)} />
            {item.category && <MetaPill value={item.category} />}
          </div>
        </div>

        <StatusBadge downloading={item.downloading} status={item.status} />

        <div className="torrentActions" onClick={(event) => event.stopPropagation()}>
          {item.magnetUrl && (
            <CopyButton value={item.magnetUrl} title="Copy MagnetLink" variant="magnet" size={17} />
          )}
        </div>
      </div>

      {open && (
        <TorrentDetails
          item={item}
          inFlightCommands={inFlightCommands}
          onDownload={onDownload}
          onDownloadAction={onDownloadAction}
        />
      )}
    </article>
  );
});

function handleToggleKeyDown(event: React.KeyboardEvent<HTMLDivElement>, onToggle: () => void) {
  if (event.key !== "Enter" && event.key !== " ") {
    return;
  }
  event.preventDefault();
  onToggle();
}

function MetaPill({
  icon,
  value,
  tone = "neutral"
}: {
  icon?: React.ReactNode;
  value: string;
  tone?: "neutral" | "strong";
}) {
  return (
    <span className={`metaPill ${tone}`}>
      {icon}
      <span>{value}</span>
    </span>
  );
}

function TorrentDetails({
  item,
  inFlightCommands,
  onDownload,
  onDownloadAction
}: {
  item: TorrentState;
  inFlightCommands: Set<string>;
  onDownload: (item: TorrentState, path: string) => Promise<void>;
  onDownloadAction: (task: TaskItem, action: DownloadAction) => Promise<void>;
}) {
  return (
    <div className="torrentDetails">
      <div className="detailsGrid overviewGrid">
        <TorrentInfoPanel item={item} />
        <RuntimePanel runtime={item.runtime} />
      </div>
      <FilePanel
        item={item}
        inFlightCommands={inFlightCommands}
        onDownload={onDownload}
        onDownloadAction={onDownloadAction}
      />
      <div className="detailsGrid runtimeGrid">
        <RuntimePeers runtime={item.runtime} />
        <RuntimeDHT runtime={item.runtime} />
      </div>
      <RuntimeEvents runtime={item.runtime} />
    </div>
  );
}

function TorrentInfoPanel({ item }: { item: TorrentItem }) {
  return (
    <DetailBox
      icon={<Database size={16} />}
      meta={statusLabels[item.status]}
      title="Torrent"
    >
      <div className="torrentInfoBody">
        <dl className="detailGrid">
          <DetailItem label="Provider" value={item.provider || "-"} />
          <DetailItem label="Size" value={formatBytes(item.bytes)} />
          <DetailItem label="Date" value={formatDate(item.date)} />
          <DetailItem label="Seeds / Peers" value={`${item.seeders}/${item.peers}`} />
          <DetailItem label="Category" value={item.category || "-"} />
        </dl>
        <CodeBlock label="Info Hash" value={item.hash || ""} />
        <CodeBlock
          action={
            item.magnetUrl ? (
              <CopyButton value={item.magnetUrl} title="Copy MagnetLink" variant="magnet" size={15} />
            ) : null
          }
          label="MagnetLink"
          value={item.magnetUrl || ""}
        />
      </div>
    </DetailBox>
  );
}

function DetailBox({
  action,
  bodyClassName = "",
  children,
  icon,
  meta,
  title
}: {
  action?: React.ReactNode;
  bodyClassName?: string;
  children: React.ReactNode;
  icon: React.ReactNode;
  meta?: string;
  title: string;
}) {
  return (
    <section className="detailBox">
      <div className="detailBoxHeader">
        <div className="detailBoxTitle">
          {icon}
          <h3>{title}</h3>
        </div>
        <div className="detailBoxActions">
          {action}
          {meta && <span className="detailBoxMeta">{meta}</span>}
        </div>
      </div>
      <div className={`detailBoxBody ${bodyClassName}`}>{children}</div>
    </section>
  );
}

function PanelEmpty({
  children,
  tone = "muted"
}: {
  children: React.ReactNode;
  tone?: "muted" | "error";
}) {
  return <div className={`panelEmpty ${tone}`}>{children}</div>;
}

function RuntimePanel({ runtime }: { runtime: RuntimeView }) {
  const snapshot = runtime.snapshot ?? null;
  const pieceInfo = snapshot ? pieceCountsFromRuns(snapshot.pieceRuns) : null;

  return (
    <DetailBox
      icon={<Activity size={16} />}
      meta={runtimeMeta(runtime)}
      title="Runtime"
    >
      {runtime.status === "error" ? (
        <PanelEmpty tone="error">{runtime.error || "Runtime unavailable"}</PanelEmpty>
      ) : !snapshot ? (
        <PanelEmpty>Runtime loading</PanelEmpty>
      ) : (
        <div className="runtimePanel">
          <RuntimeSummaryGrid summary={snapshot.summary} />
          <div className="subsection">
            <div className="subsectionHeader">
              <h4>Pieces</h4>
              <span>{pieceInfo ? `${pieceInfo.complete}/${pieceInfo.total}` : "-"}</span>
            </div>
            <PieceMap pieceRuns={snapshot.pieceRuns} />
          </div>
        </div>
      )}
    </DetailBox>
  );
}

function RuntimeSummaryGrid({ summary }: { summary: RuntimeSummary }) {
  const completed = summary.bytesCompleted;
  const total = summary.length || completed;
  const percent = total > 0 ? (completed / total) * 100 : 0;
  const progress = summary.metadataReady
    ? `${formatPercent(percent)} (${formatBytes(completed)})`
    : "Metadata pending";
  const pieces = summary.metadataReady
    ? `${summary.piecesComplete}/${summary.numPieces}`
    : "Metadata pending";

  return (
    <div className="runtimeStats">
      <DetailItem label="Progress" value={progress} />
      <DetailItem label="Active peers" value={`${summary.activePeers}`} />
      <DetailItem label="Pending / Half-open" value={`${summary.pendingPeers}/${summary.halfOpenPeers}`} />
      <DetailItem label="Seeders" value={`${summary.connectedSeeders}`} />
      <DetailItem label="Pieces" value={pieces} />
      <DetailItem label="Useful / Wasted" value={`${summary.chunksReadUseful}/${summary.chunksReadWasted}`} />
      <DetailItem label="Read" value={formatBytes(summary.bytesReadUsefulData)} />
      <DetailItem label="Written" value={formatBytes(summary.bytesWrittenData)} />
    </div>
  );
}

function runtimeMeta(runtime: RuntimeView): string {
  if (runtime.status === "error") return "error";
  if (runtime.snapshot) return formatTime(runtime.snapshot.updated);
  return "loading";
}

type FlatPiece = {
  index: number;
  state: string;
  complete: boolean;
  partial: boolean;
  hashing: boolean;
  queuedHash: boolean;
  priority: string;
};

const PieceMap = React.memo(function PieceMap({
  pieceRuns
}: {
  pieceRuns: RuntimePieceRun[];
}) {
  const [scrollTop, setScrollTop] = useState(0);
  const [width, setWidth] = useState(0);
  const ref = useRef<HTMLDivElement | null>(null);
  const pieces = useMemo(() => expandPieceRuns(pieceRuns), [pieceRuns]);
  const total = pieces.length;

  useEffect(() => {
    const node = ref.current;
    if (!node) {
      return;
    }
    const observer = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (entry) {
        setWidth(entry.contentRect.width);
      }
    });
    observer.observe(node);
    return () => observer.disconnect();
  }, []);

  if (total === 0) {
    return <PanelEmpty>No piece state</PanelEmpty>;
  }
  const layout = pieceGridLayout(pieces, total, width, scrollTop);

  return (
    <div
      className="pieceMap"
      onScroll={(event) => setScrollTop(event.currentTarget.scrollTop)}
      ref={ref}
      style={{ maxHeight: pieceGridHeight }}
      title={`${total} real BitTorrent pieces`}
    >
      <div className="pieceMapSizer" style={{ height: layout.height }}>
        <div
          className="pieceMapRows"
          style={{
            gridTemplateColumns: `repeat(${layout.columns}, ${pieceCellSize}px)`,
            top: layout.top
          }}
        >
          {layout.visible.map((piece) => (
            <span
              className={`pieceCell ${pieceVisualState(piece)}`}
              key={piece.index}
              title={pieceTitle(piece)}
            />
          ))}
        </div>
      </div>
    </div>
  );
});

type PieceLayout = {
  columns: number;
  rows: number;
  visible: FlatPiece[];
  top: number;
  height: number;
};

function pieceGridLayout(pieces: FlatPiece[], total: number, width: number, scrollTop: number): PieceLayout {
  const stride = pieceCellSize + pieceCellGap;
  const columns = Math.max(1, Math.floor((width + pieceCellGap) / stride));
  const rows = Math.ceil(total / columns);
  const firstRow = Math.max(0, Math.floor(scrollTop / stride) - pieceGridOverscanRows);
  const visibleRows = Math.ceil(pieceGridHeight / stride) + pieceGridOverscanRows * 2;
  const lastRow = Math.min(rows, firstRow + visibleRows);
  const start = firstRow * columns;
  const end = Math.min(total, lastRow * columns);
  return { columns, rows, visible: pieces.slice(start, end), top: firstRow * stride, height: rows * stride };
}

function expandPieceRuns(runs: RuntimePieceRun[]): FlatPiece[] {
  const pieces: FlatPiece[] = [];
  let index = 0;
  for (const run of runs) {
    for (let i = 0; i < run.length; i++) {
      pieces.push({ index, state: run.state, complete: run.complete, partial: run.partial, hashing: run.hashing, queuedHash: run.queuedHash, priority: run.priority });
      index++;
    }
  }
  return pieces;
}

function pieceCountsFromRuns(runs: RuntimePieceRun[]): { complete: number; total: number } {
  let complete = 0;
  let total = 0;
  for (const run of runs) {
    total += run.length;
    if (run.complete) complete += run.length;
  }
  return { complete, total };
}

function pieceVisualState(piece: FlatPiece): string {
  if (piece.complete) return "complete";
  if (piece.hashing || piece.queuedHash) return "hashing";
  return "empty";
}

function pieceTitle(piece: FlatPiece): string {
  return `Piece ${piece.index}\nState: ${piece.state || pieceVisualState(piece)}\nPriority: ${piece.priority || "none"}`;
}

function RuntimeDHT({ runtime }: { runtime: RuntimeView }) {
  const snapshot = runtime.snapshot ?? null;
  const servers = snapshot?.dht ?? [];
  return (
    <DetailBox
      bodyClassName="flush"
      icon={<RadioTower size={16} />}
      meta={runtime.status === "ready" ? `${servers.length}` : runtimeMeta(runtime)}
      title="DHT"
    >
      {runtime.status === "error" ? (
        <PanelEmpty tone="error">{runtime.error || "Runtime unavailable"}</PanelEmpty>
      ) : !snapshot ? (
        <PanelEmpty>Runtime loading</PanelEmpty>
      ) : servers.length === 0 ? (
        <PanelEmpty>DHT unavailable</PanelEmpty>
      ) : (
        <div className="dataList">
          {servers.map((server) => (
            <div className="dhtRow" key={`${server.id}-${server.address}`}>
              <RadioTower size={15} />
              <span className="monoText">{server.address}</span>
              <span>{server.goodNodes}/{server.nodes} good</span>
              <span>{server.outstandingTransactions} tx</span>
              <span>{server.badNodes} bad</span>
            </div>
          ))}
        </div>
      )}
    </DetailBox>
  );
}

function RuntimePeers({ runtime }: { runtime: RuntimeView }) {
  const snapshot = runtime.snapshot ?? null;
  const peers = snapshot?.peers ?? [];
  return (
    <DetailBox
      bodyClassName="flush"
      icon={<Network size={16} />}
      meta={runtime.status === "ready" ? `${peers.length}` : runtimeMeta(runtime)}
      title="Peers"
    >
      {runtime.status === "error" ? (
        <PanelEmpty tone="error">{runtime.error || "Runtime unavailable"}</PanelEmpty>
      ) : !snapshot ? (
        <PanelEmpty>Runtime loading</PanelEmpty>
      ) : peers.length === 0 ? (
        <PanelEmpty>No peers</PanelEmpty>
      ) : (
        <div className="dataList">
          {peers.slice(0, 18).map((peer) => (
            <div className="peerRow" key={`${peer.address}-${peer.network}-${peer.connected}`}>
              <Network size={15} />
              <span className="monoText">{peer.address}</span>
              <span>{peer.connected ? peer.network || "peer" : "known"}</span>
              <span>{peerSourceLabel(peer.source)}</span>
              <span>{formatBytes(peer.downloadRate)}/s</span>
              <span>{peer.client || "-"}</span>
            </div>
          ))}
        </div>
      )}
    </DetailBox>
  );
}

function RuntimeEvents({ runtime }: { runtime: RuntimeView }) {
  const snapshot = runtime.snapshot ?? null;
  const events = snapshot?.events ?? [];
  const recent = events.slice(-12).reverse();
  return (
    <DetailBox
      bodyClassName="flush"
      icon={<Calendar size={16} />}
      meta={runtime.status === "ready" ? `${events.length}` : runtimeMeta(runtime)}
      title="Events"
    >
      {runtime.status === "error" ? (
        <PanelEmpty tone="error">{runtime.error || "Runtime unavailable"}</PanelEmpty>
      ) : !snapshot ? (
        <PanelEmpty>Runtime loading</PanelEmpty>
      ) : recent.length === 0 ? (
        <PanelEmpty>No events</PanelEmpty>
      ) : (
        <div className="dataList">
          {recent.map((event, index) => (
            <div className="eventRow" key={`${event.time}-${event.type}-${index}`}>
              <span>{formatTime(event.time)}</span>
              <span>{eventLabel(event)}</span>
            </div>
          ))}
        </div>
      )}
    </DetailBox>
  );
}

function DetailItem({ label, value }: { label: string; value: string }) {
  return (
    <div className="detailItem">
      <dt>{label}</dt>
      <dd>{value}</dd>
    </div>
  );
}

function CodeBlock({
  label,
  action,
  value
}: {
  label: string;
  action?: React.ReactNode;
  value: string;
}) {
  return (
    <div className="codeBlock">
      <div className="codeHeader">
        <span>{label}</span>
        {action}
      </div>
      <pre>{value}</pre>
    </div>
  );
}

function StatusBadge({ downloading, status }: { downloading: boolean; status: TorrentStatus }) {
  if (downloading) {
    return (
      <span className="status downloading">
        <span className="spinner" aria-hidden="true" />
        Downloading
      </span>
    );
  }
  const pending = status === "idle" || status === "loading";
  return (
    <span className={`status ${status}`}>
      {pending && <span className="spinner" aria-hidden="true" />}
      {statusLabels[status]}
    </span>
  );
}

function FilePanel({
  item,
  inFlightCommands,
  onDownload,
  onDownloadAction
}: {
  item: TorrentState;
  inFlightCommands: Set<string>;
  onDownload: (item: TorrentState, path: string) => Promise<void>;
  onDownloadAction: (task: TaskItem, action: DownloadAction) => Promise<void>;
}) {
  const files = item.files ?? [];
  const meta = item.status === "ready" ? `${files.length}` : statusLabels[item.status];
  const [selectedPaths, setSelectedPaths] = useState<Set<string>>(() => new Set());

  useEffect(() => {
    setSelectedPaths((current) => {
      const available = new Set(files.map((file) => file.path));
      let changed = false;
      const next = new Set<string>();
      for (const path of current) {
        if (available.has(path)) {
          next.add(path);
        } else {
          changed = true;
        }
      }
      return changed ? next : current;
    });
  }, [files]);

  const selectedFiles = useMemo(
    () => files.filter((file) => selectedPaths.has(file.path)),
    [files, selectedPaths]
  );
  const selectableFiles = files;
  const allSelected = selectableFiles.length > 0 && selectableFiles.every((file) => selectedPaths.has(file.path));
  const someSelected = selectedPaths.size > 0 && !allSelected;

  const toggleAllFiles = useCallback(() => {
    setSelectedPaths((current) => {
      if (selectableFiles.length > 0 && selectableFiles.every((file) => current.has(file.path))) {
        return new Set();
      }
      return new Set(selectableFiles.map((file) => file.path));
    });
  }, [selectableFiles]);

  const toggleFile = useCallback((path: string) => {
    setSelectedPaths((current) => {
      const next = new Set(current);
      if (next.has(path)) {
        next.delete(path);
      } else {
        next.add(path);
      }
      return next;
    });
  }, []);

  const runBatchAction = useCallback(
    async (action: BatchAction) => {
      for (const file of selectedFiles) {
        if (action === "download") {
          if (canStartFileDownload(item, file, inFlightCommands)) {
            await onDownload(item, file.path);
          }
          continue;
        }
        if (file.task && canRunTaskAction(file.task, action, inFlightCommands)) {
          await onDownloadAction(file.task, action);
        }
      }
    },
    [inFlightCommands, item, onDownload, onDownloadAction, selectedFiles]
  );

  const batchAvailability = useMemo(() => ({
    download: selectedFiles.some((file) => canStartFileDownload(item, file, inFlightCommands)),
    pause: selectedFiles.some((file) => file.task !== undefined && canRunTaskAction(file.task, "pause", inFlightCommands)),
    resume: selectedFiles.some((file) => file.task !== undefined && canRunTaskAction(file.task, "resume", inFlightCommands)),
    cancel: selectedFiles.some((file) => file.task !== undefined && canRunTaskAction(file.task, "cancel", inFlightCommands)),
    delete: selectedFiles.some((file) => file.task !== undefined && canRunTaskAction(file.task, "delete", inFlightCommands))
  }), [inFlightCommands, item, selectedFiles]);

  if (item.status === "unavailable" || item.status === "error") {
    return (
      <DetailBox
        icon={<FileText size={16} />}
        meta={meta}
        title="Files"
      >
        <PanelEmpty tone="error">{item.error || "Unavailable"}</PanelEmpty>
      </DetailBox>
    );
  }
  if (item.status === "idle" || item.status === "loading") {
    return (
      <DetailBox
        icon={<FileText size={16} />}
        meta={meta}
        title="Files"
      >
        <PanelEmpty>Metadata pending</PanelEmpty>
      </DetailBox>
    );
  }
  if (files.length === 0) {
    return (
      <DetailBox
        icon={<FileText size={16} />}
        meta={meta}
        title="Files"
      >
        <PanelEmpty>No files</PanelEmpty>
      </DetailBox>
    );
  }
  return (
    <DetailBox
      bodyClassName="flush"
      icon={<FileText size={16} />}
      meta={meta}
      title="Files"
    >
      <div className="fileToolbar">
        <label className="selectControl" title={allSelected ? "Clear selection" : "Select files"}>
          <input
            checked={allSelected}
            onChange={toggleAllFiles}
            ref={(input) => {
              if (input) {
                input.indeterminate = someSelected;
              }
            }}
            type="checkbox"
          />
        </label>
        <span className="selectionMeta">{selectedPaths.size > 0 ? `${selectedPaths.size} selected` : `${files.length} files`}</span>
        {selectedPaths.size > 0 && (
          <div className="bulkActions">
            <button className="iconButton" disabled={!batchAvailability.download} onClick={() => { void runBatchAction("download"); }} title="Download selected">
              <Download size={16} />
            </button>
            <button className="iconButton" disabled={!batchAvailability.pause} onClick={() => { void runBatchAction("pause"); }} title="Pause selected">
              <Pause size={16} />
            </button>
            <button className="iconButton" disabled={!batchAvailability.resume} onClick={() => { void runBatchAction("resume"); }} title="Resume selected">
              <Play size={16} />
            </button>
            <button className="iconButton" disabled={!batchAvailability.cancel} onClick={() => { void runBatchAction("cancel"); }} title="Cancel selected">
              <X size={16} />
            </button>
            <button className="iconButton" disabled={!batchAvailability.delete} onClick={() => { void runBatchAction("delete"); }} title="Delete selected tasks">
              <Trash2 size={16} />
            </button>
          </div>
        )}
      </div>
      <div className="assetList">
        {files.map((file) => {
          const task = file.task;
          const busy = isFileDownloadBusy(item, file, inFlightCommands);
          const disabled = busy || file.status === "complete" || task !== undefined;
          const selected = selectedPaths.has(file.path);
          return (
            <article className={`assetRow ${selected ? "selected" : ""}`} key={file.path}>
              <div className="assetSelect">
                <label className="selectControl" title={selected ? "Deselect file" : "Select file"}>
                  <input
                    checked={selected}
                    onChange={() => toggleFile(file.path)}
                    type="checkbox"
                  />
                </label>
              </div>
              <div className="assetBody">
                <div className="assetName">{file.path}</div>
                <div className="assetMeta">
                  <span>{formatBytes(file.bytes)}</span>
                  <span>{fileProgressLabel(file)}</span>
                  <FileStatePill file={file} task={task} />
                </div>
                <pre className="assetPath">{file.savePath}</pre>
              </div>
              <div className="assetActions">
                <CopyButton value={file.savePath} title="Copy path" variant="url" size={16} />
                {task ? (
                  <DownloadTaskActions
                    task={task}
                    inFlightCommands={inFlightCommands}
                    onAction={onDownloadAction}
                  />
                ) : (
                  <button
                    className="iconButton"
                    disabled={disabled}
                    onClick={() => { void onDownload(item, file.path); }}
                    title={file.status === "complete" ? "Downloaded" : busy ? "Downloading" : "Download"}
                  >
                    {downloadButtonIcon(file.status === "complete", busy)}
                  </button>
                )}
              </div>
            </article>
          );
        })}
      </div>
    </DetailBox>
  );
}

function downloadButtonIcon(complete: boolean, busy: boolean): React.ReactNode {
  if (complete) {
    return <Check size={16} />;
  }
  if (busy) {
    return <span className="spinner" aria-hidden="true" />;
  }
  return <Download size={16} />;
}

function canStartFileDownload(item: TorrentItem, file: FileItem, inFlightCommands: Set<string>): boolean {
  return !isFileDownloadBusy(item, file, inFlightCommands) &&
    file.status !== "complete" &&
    file.task === undefined;
}

function isFileDownloadBusy(item: TorrentItem, file: FileItem, inFlightCommands: Set<string>): boolean {
  return inFlightCommands.has(downloadKey(item.id, file.path)) || file.status === "downloading";
}

function canRunTaskAction(task: TaskItem, action: DownloadAction, inFlightCommands: Set<string>): boolean {
  if (inFlightCommands.has(taskCommandKey(task.id, action))) {
    return false;
  }
  switch (action) {
    case "pause":
    case "cancel":
      return task.status === "downloading";
    case "resume":
      return task.status === "paused" || task.status === "canceled";
    case "delete":
      return true;
  }
}

function fileProgressLabel(file: FileItem): string {
  if (file.bytes <= 0) {
    return "-";
  }
  if (!file.task && file.status === "cached") {
    return `Cached ${formatPercent((file.cachedBytes / file.bytes) * 100)} (${formatBytes(file.cachedBytes)})`;
  }
  const completed = file.task?.completedBytes ?? file.completedBytes;
  return `${formatPercent((completed / file.bytes) * 100)} (${formatBytes(completed)})`;
}

function FileStatePill({ file, task }: { file: FileItem; task?: TaskItem }) {
  if (task?.status === "paused") return <span className="statePill paused">Paused</span>;
  if (task?.status === "canceled") return <span className="statePill canceled">Canceled</span>;
  if (file.status === "downloading") return <span className="statePill downloading">Downloading</span>;
  if (file.status === "complete") return <span className="statePill complete">Complete</span>;
  if (file.status === "cached") return <span className="statePill cached">Cached</span>;
  return null;
}

function DownloadTaskActions({
  task,
  inFlightCommands,
  onAction
}: {
  task: TaskItem;
  inFlightCommands: Set<string>;
  onAction: (task: TaskItem, action: DownloadAction) => Promise<void>;
}) {
  const busy = (action: DownloadAction) => inFlightCommands.has(taskCommandKey(task.id, action));
  if (task.status === "downloading") {
    return (
      <>
        <TaskActionButton action="pause" busy={busy("pause")} task={task} icon={<Pause size={16} />} onAction={onAction} title="Pause" />
        <TaskActionButton action="cancel" busy={busy("cancel")} task={task} icon={<X size={16} />} onAction={onAction} title="Cancel" />
      </>
    );
  }
  if (task.status === "paused" || task.status === "canceled") {
    return (
      <>
        <TaskActionButton action="resume" busy={busy("resume")} task={task} icon={<Play size={16} />} onAction={onAction} title="Resume" />
        <TaskActionButton action="delete" busy={busy("delete")} task={task} icon={<Trash2 size={16} />} onAction={onAction} title="Delete task" />
      </>
    );
  }
  return (
    <TaskActionButton action="delete" busy={busy("delete")} task={task} icon={<Trash2 size={16} />} onAction={onAction} title="Delete task" />
  );
}

function TaskActionButton({
  action,
  busy,
  task,
  icon,
  onAction,
  title
}: {
  action: DownloadAction;
  busy: boolean;
  task: TaskItem;
  icon: React.ReactNode;
  onAction: (task: TaskItem, action: DownloadAction) => Promise<void>;
  title: string;
}) {
  const disabled = busy || !canRunTaskAction(task, action, new Set());
  return (
    <button
      className="iconButton"
      disabled={disabled}
      onClick={() => { void onAction(task, action); }}
      title={title}
    >
      {busy ? <span className="spinner" aria-hidden="true" /> : icon}
    </button>
  );
}

function CopyButton({
  value,
  title,
  variant,
  size
}: {
  value: string;
  title: string;
  variant: "magnet" | "url";
  size: number;
}) {
  const [copied, setCopied] = useState(false);
  const timeoutRef = useRef<number | null>(null);

  useEffect(() => {
    return () => {
      if (timeoutRef.current !== null) {
        window.clearTimeout(timeoutRef.current);
      }
    };
  }, []);

  async function handleCopy() {
    try {
      await copyText(value);
      setCopied(true);
      if (timeoutRef.current !== null) {
        window.clearTimeout(timeoutRef.current);
      }
      timeoutRef.current = window.setTimeout(() => setCopied(false), 900);
    } catch {
      setCopied(false);
    }
  }

  const Icon = copied ? Check : variant === "magnet" ? Magnet : Link;

  return (
    <button
      className={`iconButton copyButton ${copied ? "copied" : ""}`}
      onClick={() => void handleCopy()}
      title={copied ? "Copied" : title}
    >
      <Icon size={size} />
    </button>
  );
}

function filterTorrents(items: TorrentState[], filter: string): TorrentState[] {
  const needle = filter.trim().toLowerCase();
  if (!needle) {
    return items;
  }
  return items.filter((item) =>
    [
      item.title,
      item.provider,
      item.hash ?? "",
      item.magnetUrl ?? "",
      item.category ?? ""
    ].some((value) => value.toLowerCase().includes(needle))
  );
}

function downloadKey(id: string, path = ""): string {
  return `${id}\0${path}`;
}

function taskCommandKey(id: string, action: DownloadAction): string {
  return `${id}\0${action}`;
}

function addInFlightCommand(
  inFlight: Set<string>,
  setInFlight: React.Dispatch<React.SetStateAction<Set<string>>>,
  key: string
) {
  inFlight.add(key);
  setInFlight((current) => new Set(current).add(key));
}

function clearInFlightCommand(
  pending: Set<string>,
  setPending: React.Dispatch<React.SetStateAction<Set<string>>>,
  key: string
) {
  pending.delete(key);
  setPending((current) => {
    if (!current.has(key)) {
      return current;
    }
    const next = new Set(current);
    next.delete(key);
    return next;
  });
}

function stateEndpoint(): string {
  return "/api/state";
}

function stateStreamEndpoint(): string {
  return "/api/state/stream";
}

function torrentURL(id: string): string {
  return `/api/torrents/${encodeURIComponent(id)}`;
}

function torrentFileDownloadEndpoint(id: string): string {
  return `${torrentURL(id)}/files/download`;
}

function downloadActionEndpoint(id: string, action: DownloadAction): string {
  return `/api/downloads/${encodeURIComponent(id)}/${action}`;
}

async function getJSON<T>(url: string): Promise<T> {
  let response: Response;
  try {
    response = await fetch(url);
  } catch (err) {
    throw new Error(`HTTP service unavailable: ${errorMessage(err)}`);
  }
  if (!response.ok) {
    throw new HTTPError(response.status, await response.text());
  }
  return (await response.json()) as T;
}

async function postJSON<T>(url: string, body: unknown): Promise<T> {
  let response: Response;
  try {
    response = await fetch(url, {
      body: JSON.stringify(body),
      headers: { "Content-Type": "application/json" },
      method: "POST"
    });
  } catch (err) {
    throw new Error(`HTTP service unavailable: ${errorMessage(err)}`);
  }
  if (!response.ok) {
    throw new HTTPError(response.status, await response.text());
  }
  return (await response.json()) as T;
}

async function copyText(value: string) {
  await navigator.clipboard.writeText(value);
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

function serviceErrorMessage(err: unknown): string {
  if (err instanceof HTTPError) {
    return `HTTP service error (${err.status}): ${err.message}`;
  }
  return errorMessage(err);
}

function formatDate(value?: string): string {
  if (!value) {
    return "-";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleDateString(undefined, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit"
  });
}

function formatTime(value?: string): string {
  if (!value) {
    return "-";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleTimeString(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit"
  });
}

function formatPercent(value: number): string {
  if (!Number.isFinite(value)) {
    return "-";
  }
  return `${value.toFixed(value >= 99.95 ? 0 : 1)}%`;
}

function formatBytes(value?: number): string {
  if (!value || value <= 0) {
    return "-";
  }
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let size = value;
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024;
    unit++;
  }
  return `${size.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}

function peerSourceLabel(value: string): string {
  switch (value) {
    case "Tr":
      return "tracker";
    case "Hg":
      return "dht";
    case "Ha":
      return "dht announce";
    case "X":
      return "pex";
    case "I":
      return "incoming";
    case "M":
      return "magnet";
    case "L":
      return "local";
    case "C":
      return "holepunch";
    default:
      return value || "-";
  }
}

function eventLabel(event: RuntimeTorrentEvent): string {
  if (event.type === "chunk_received") {
    return `${event.peer || "-"} piece ${event.piece} +${event.begin} ${formatBytes(event.length)}`;
  }
  if (event.type === "request_sent") {
    return `request ${event.peer || "-"} piece ${event.piece} ${formatBytes(event.length)}`;
  }
  if (event.type === "dht_query") {
    return `DHT ${event.dhtQuery || "query"} ${event.dhtNode || ""}`.trim();
  }
  if (event.error) {
    return `${event.type}: ${event.error}`;
  }
  return [event.type, event.peer, event.client].filter(Boolean).join(" ");
}

createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
);
