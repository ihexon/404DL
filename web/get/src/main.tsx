import React, { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  Activity,
  Calendar,
  Check,
  ChevronRight,
  Database,
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
  Users
} from "lucide-react";
import "./styles.css";

type FileStatus = "idle" | "downloading" | "complete";
type TorrentDownloadStatus = "idle" | "downloading" | "paused" | "complete";

type FileItem = {
  path: string;
  bytes: number;
  completedBytes: number;
  savePath: string;
  status: FileStatus;
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
  downloading: boolean;
  download: DownloadView;
  error?: string;
  files?: FileItem[];
};

type SearchResult = {
  provider: string;
  title: string;
  bytes?: number;
  category?: string;
  date?: string;
  seeders: number;
  peers: number;
  hash?: string;
  magnetUrl?: string;
};

type AppState = {
  updated: string;
  saveTo: string;
  searchResults: SearchResult[];
  torrents: TorrentState[];
};

type DownloadView = {
  status: TorrentDownloadStatus;
  completedBytes: number;
  bytes: number;
};

type TorrentState = TorrentItem & {
  runtime: RuntimeView;
};

type TorrentListItem = TorrentState & {
  source: "download" | "search";
  searchResult?: SearchResult;
};

type RuntimeView = {
  status: "inactive" | "ready" | "error";
  snapshot?: RuntimeSnapshot;
  error?: string;
};

type TorrentStore = {
  downloads: TorrentState[];
  error: string;
  inFlightCommands: Set<string>;
  searchResults: SearchResult[];
  downloadSearchResult: (result: SearchResult) => Promise<void>;
  loadTorrent: (id: string) => Promise<void>;
  runTorrentAction: (item: TorrentState, action: TorrentAction) => Promise<void>;
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
  downloadRate: number;
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

type TorrentAction = "start" | "pause" | "delete";
type TorrentView = "search" | "downloading" | "complete";

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

const pieceCellSize = 11;
const pieceCellGap = 3;
const pieceGridHeight = 260;
const pieceGridOverscanRows = 4;
const fallbackPieceMapColumns = 32;

function App() {
  const {
    downloads,
    downloadSearchResult,
    error,
    inFlightCommands,
    loadTorrent,
    runTorrentAction,
    searchResults
  } = useTorrents();
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [view, setView] = useState<TorrentView>("search");

  const items = useMemo(() => torrentListItems(searchResults, downloads, view), [downloads, searchResults, view]);

  function toggle(item: TorrentListItem) {
    const willOpen = !expanded[item.id];
    setExpanded((current) => ({ ...current, [item.id]: willOpen }));
    if (!willOpen || item.source !== "download") {
      return;
    }
    void loadTorrent(item.id);
  }

  async function runListAction(item: TorrentListItem, action: TorrentAction) {
    if (item.source === "search") {
      if (action === "start" && item.searchResult) {
        await downloadSearchResult(item.searchResult);
      }
      return;
    }
    await runTorrentAction(item, action);
  }

  return (
    <main className="shell">
      <header className="topbar">
        <div>
          <h1>4dl</h1>
          <div className="meta">{downloads.length} downloads</div>
        </div>
      </header>

      {error && <div className="banner">{error}</div>}

      <SearchPanel
        onSearchStart={() => {
          setView("search");
          setExpanded({});
        }}
      />

      <div className="listToolbar">
        <div className="viewTabs" aria-label="Torrent view">
          <ViewTab current={view} onChange={setView} value="search">Search</ViewTab>
          <ViewTab current={view} onChange={setView} value="downloading">Downloading</ViewTab>
          <ViewTab current={view} onChange={setView} value="complete">Complete</ViewTab>
        </div>
      </div>

      <TorrentList
        items={items}
        inFlightCommands={inFlightCommands}
        expanded={expanded}
        onTorrentAction={runListAction}
        onToggle={toggle}
      />
    </main>
  );
}

function useTorrents(): TorrentStore {
  const [state, setState] = useState<AppState | null>(null);
  const [detailsByID, setDetailsByID] = useState<Map<string, TorrentState>>(() => new Map());
  const [error, setError] = useState("");
  const [inFlightCommands, setInFlightCommands] = useState<Set<string>>(new Set());
  const inFlightCommandsRef = useRef<Set<string>>(new Set());
  const detailsRef = useRef<Map<string, TorrentState>>(new Map());

  useEffect(() => {
    detailsRef.current = detailsByID;
  }, [detailsByID]);

  const mergeAppState = useCallback((next: AppState) => {
    const details = detailsRef.current;
    next.torrents = next.torrents.map((item) => mergeListTorrent(item, details.get(item.id)));
    setState(next);
  }, []);

  const mergeTorrent = useCallback((next: TorrentState) => {
    setDetailsByID((current) => {
      const updated = new Map(current);
      updated.set(next.id, next);
      return updated;
    });
    setState((current) => {
      if (!current) {
        return current;
      }
      return {
        ...current,
        torrents: current.torrents.map((item) => item.id === next.id ? next : item)
      };
    });
  }, []);

  const loadTorrent = useCallback(
    async (id: string) => {
      const next = await getJSON<TorrentState>(torrentEndpoint(id));
      mergeTorrent(next);
    },
    [mergeTorrent]
  );

  const showServiceError = useCallback((err: unknown) => {
    setError(serviceErrorMessage(err));
  }, []);

  const runTorrentAction = useCallback(
    async (item: TorrentState, action: TorrentAction) => {
      const commandKey = torrentActionKey(item.id, action);
      if (inFlightCommandsRef.current.has(commandKey)) {
        return;
      }
      addInFlightCommand(inFlightCommandsRef.current, setInFlightCommands, commandKey);
      setError("");
      try {
        const next = await postJSON<TorrentState>(torrentActionEndpoint(item.id, action), {});
        mergeTorrent(next);
      } catch (err) {
        showServiceError(err);
      } finally {
        clearInFlightCommand(inFlightCommandsRef.current, setInFlightCommands, commandKey);
      }
    },
    [mergeTorrent, showServiceError]
  );

  const downloadSearchResult = useCallback(
    async (result: SearchResult) => {
      if (!result.hash) {
        return;
      }
      const commandKey = searchResultActionKey(result);
      if (inFlightCommandsRef.current.has(commandKey)) {
        return;
      }
      addInFlightCommand(inFlightCommandsRef.current, setInFlightCommands, commandKey);
      setError("");
      try {
        const item = await postJSON<TorrentState>(torrentListEndpoint(), { result });
        mergeTorrent(item);
        const next = await postJSON<TorrentState>(torrentActionEndpoint(item.id, "start"), {});
        mergeTorrent(next);
      } catch (err) {
        showServiceError(err);
      } finally {
        clearInFlightCommand(inFlightCommandsRef.current, setInFlightCommands, commandKey);
      }
    },
    [mergeTorrent, showServiceError]
  );

  useEffect(() => {
    const source = new EventSource(torrentListStreamEndpoint());
    source.addEventListener("open", () => setError(""));
    source.addEventListener("state", (event) => {
      mergeAppState(JSON.parse(event.data) as AppState);
    });
    return () => source.close();
  }, [mergeAppState]);

  return {
    downloads: state?.torrents ?? [],
    error,
    inFlightCommands,
    searchResults: state?.searchResults ?? [],
    downloadSearchResult,
    loadTorrent,
    runTorrentAction
  };
}

function SearchPanel({ onSearchStart }: { onSearchStart: () => void }) {
  const [query, setQuery] = useState("");
  const [limit, setLimit] = useState(50);
  const [searching, setSearching] = useState(false);
  const [error, setError] = useState("");

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const normalized = query.trim();
    if (!normalized || searching) {
      return;
    }
    setSearching(true);
    setError("");
    onSearchStart();
    try {
      await postJSON<SearchResult[]>(searchEndpoint(), { query: normalized, limit });
    } catch (err) {
      setError(serviceErrorMessage(err));
    } finally {
      setSearching(false);
    }
  }

  return (
    <section className="queryPanel">
      <form className="queryForm" onSubmit={(event) => { void submit(event); }}>
        <label className="queryField">
          <Search size={16} />
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Search torrents"
          />
        </label>
        <label className="limitField">
          <span>Limit</span>
          <input
            min={1}
            max={200}
            type="number"
            value={limit}
            onChange={(event) => setLimit(Number(event.target.value) || 1)}
          />
        </label>
        <button className="primaryButton" disabled={searching || query.trim() === ""} type="submit">
          {searching ? <span className="spinner" aria-hidden="true" /> : <Search size={16} />}
          <span>Search</span>
        </button>
      </form>
      {error && <div className="inlineError">{error}</div>}
    </section>
  );
}

function TorrentList({
  items,
  inFlightCommands,
  expanded,
  onTorrentAction,
  onToggle
}: {
  items: TorrentListItem[];
  inFlightCommands: Set<string>;
  expanded: Record<string, boolean>;
  onTorrentAction: (item: TorrentListItem, action: TorrentAction) => Promise<void>;
  onToggle: (item: TorrentListItem) => void;
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
          onTorrentAction={onTorrentAction}
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
  onTorrentAction,
  onToggle
}: {
  item: TorrentListItem;
  inFlightCommands: Set<string>;
  open: boolean;
  onTorrentAction: (item: TorrentListItem, action: TorrentAction) => Promise<void>;
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

        <DownloadStatus status={item.download.status} />

        <div className="torrentActions" onClick={(event) => event.stopPropagation()}>
          <TorrentActionButton
            action="start"
            icon={<Play size={16} />}
            inFlightCommands={inFlightCommands}
            item={item}
            onAction={onTorrentAction}
            title={item.source === "search" ? "Start download" : item.download.status === "paused" ? "Continue" : "Start"}
          />
          {item.source === "download" && (
            <>
              <TorrentActionButton
                action="pause"
                icon={<Pause size={16} />}
                inFlightCommands={inFlightCommands}
                item={item}
                onAction={onTorrentAction}
                title="Pause"
              />
              <TorrentActionButton
                action="delete"
                icon={<Trash2 size={16} />}
                inFlightCommands={inFlightCommands}
                item={item}
                onAction={onTorrentAction}
                title="Delete local data"
              />
            </>
          )}
          {item.magnetUrl && (
            <CopyButton value={item.magnetUrl} title="Copy MagnetLink" variant="magnet" size={17} />
          )}
        </div>
      </div>

      {open && (
        <TorrentDetails
          item={item}
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

function TorrentDetails({ item }: { item: TorrentState }) {
  return (
    <div className="torrentDetails">
      <div className="detailsGrid overviewGrid">
        <TorrentInfoPanel item={item} />
        <RuntimePanel runtime={item.runtime} />
      </div>
      <FilePanel item={item} />
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
      ) : runtime.status === "inactive" ? (
        <PanelEmpty>Runtime inactive</PanelEmpty>
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
  const progress = summary.metadataReady
    ? `${formatBytes(completed)} / ${formatBytes(total)}`
    : "-";
  const pieces = summary.metadataReady
    ? `${summary.piecesComplete}/${summary.numPieces}`
    : "-";

  return (
    <div className="runtimeStats">
      <DetailItem label="Progress" value={progress} />
      <DetailItem label="Download speed" value={formatRate(summary.downloadRate)} />
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
  return "inactive";
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

  useLayoutEffect(() => {
    const node = ref.current;
    if (!node) {
      return;
    }
    const updateWidth = (next: number) => {
      if (next > 0) {
        setWidth(next);
      }
    };
    updateWidth(measuredPieceMapWidth(node));
    const observer = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (entry) {
        updateWidth(entry.contentRect.width || measuredPieceMapWidth(node));
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
  const measuredColumns = Math.floor((width + pieceCellGap) / stride);
  const columns = Math.max(2, measuredColumns || Math.min(total, fallbackPieceMapColumns));
  const rows = Math.ceil(total / columns);
  const firstRow = Math.max(0, Math.floor(scrollTop / stride) - pieceGridOverscanRows);
  const visibleRows = Math.ceil(pieceGridHeight / stride) + pieceGridOverscanRows * 2;
  const lastRow = Math.min(rows, firstRow + visibleRows);
  const start = firstRow * columns;
  const end = Math.min(total, lastRow * columns);
  return { columns, rows, visible: pieces.slice(start, end), top: firstRow * stride, height: rows * stride };
}

function measuredPieceMapWidth(node: HTMLDivElement): number {
  const style = window.getComputedStyle(node);
  const padding = parseFloat(style.paddingLeft) + parseFloat(style.paddingRight);
  return Math.max(0, node.clientWidth - padding);
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
      ) : runtime.status === "inactive" ? (
        <PanelEmpty>DHT inactive</PanelEmpty>
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
      ) : runtime.status === "inactive" ? (
        <PanelEmpty>No active runtime</PanelEmpty>
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
              <span>{formatRate(peer.downloadRate)}</span>
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
      ) : runtime.status === "inactive" ? (
        <PanelEmpty>No active runtime</PanelEmpty>
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

function ViewTab({
  children,
  current,
  onChange,
  value
}: {
  children: React.ReactNode;
  current: TorrentView;
  onChange: (value: TorrentView) => void;
  value: TorrentView;
}) {
  return (
    <button
      className={`viewTab ${current === value ? "active" : ""}`}
      onClick={() => onChange(value)}
      type="button"
    >
      {children}
    </button>
  );
}

function DownloadStatus({ status }: { status: TorrentDownloadStatus }) {
  if (status === "downloading") {
    return (
      <span className="status downloading">
        <span className="spinner" aria-hidden="true" />
        Downloading
      </span>
    );
  }
  if (status !== "complete") {
    return null;
  }
  return (
    <span className="status complete">
      <Check size={14} />
      Complete
    </span>
  );
}

function TorrentActionButton({
  action,
  icon,
  inFlightCommands,
  item,
  onAction,
  title
}: {
  action: TorrentAction;
  icon: React.ReactNode;
  inFlightCommands: Set<string>;
  item: TorrentListItem;
  onAction: (item: TorrentListItem, action: TorrentAction) => Promise<void>;
  title: string;
}) {
  const commandKey = torrentActionKey(item.id, action);
  const searchCommandKey = item.hash ? searchResultHashActionKey(item.hash) : "";
  const busy = inFlightCommands.has(commandKey) || (action === "start" && searchCommandKey !== "" && inFlightCommands.has(searchCommandKey));
  const disabled = busy || !canRunTorrentAction(item, action);
  return (
    <button
      className={`iconButton torrentAction ${action}`}
      disabled={disabled}
      onClick={() => { void onAction(item, action); }}
      title={title}
    >
      {busy ? <span className="spinner" aria-hidden="true" /> : icon}
    </button>
  );
}

function canRunTorrentAction(item: TorrentListItem, action: TorrentAction): boolean {
  if (!item.hash) {
    return false;
  }
  if (item.source === "search") {
    return action === "start";
  }
  switch (action) {
    case "start":
      return item.download.status !== "downloading" && item.download.status !== "complete";
    case "pause":
      return item.download.status === "downloading";
    case "delete":
      return item.download.status !== "idle" || (item.files ?? []).some((file) => file.completedBytes > 0);
  }
}

function FilePanel({ item }: { item: TorrentState }) {
  const files = item.files ?? [];
  const meta = files.length > 0 ? `${files.length}` : undefined;

  if (item.error) {
    return (
      <DetailBox
        icon={<FileText size={16} />}
        meta={meta}
        title="Files"
      >
        <PanelEmpty tone="error">{item.error || "No file metadata"}</PanelEmpty>
      </DetailBox>
    );
  }
  if (files.length === 0) {
    return null;
  }
  return (
    <DetailBox
      bodyClassName="flush"
      icon={<FileText size={16} />}
      meta={meta}
      title="Files"
    >
      <div className="assetList">
        {files.map((file) => (
            <article className="assetRow" key={file.path}>
              <div className="assetBody">
                <div className="assetName">{file.path}</div>
                <div className="assetMeta">
                  <span>{formatBytes(file.bytes)}</span>
                  <span>{fileProgressLabel(file)}</span>
                  <FileStatePill file={file} />
                </div>
                <pre className="assetPath">{file.savePath}</pre>
              </div>
              <div className="assetActions">
                <CopyButton value={file.savePath} title="Copy path" variant="url" size={16} />
              </div>
            </article>
        ))}
      </div>
    </DetailBox>
  );
}

function fileProgressLabel(file: FileItem): string {
  if (file.bytes <= 0) {
    return "-";
  }
  const completed = file.completedBytes;
  return `${formatPercent((completed / file.bytes) * 100)} (${formatBytes(completed)})`;
}

function FileStatePill({ file }: { file: FileItem }) {
  if (file.status === "downloading") return <span className="statePill downloading">Downloading</span>;
  if (file.status === "complete") return <span className="statePill complete">Complete</span>;
  return null;
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

function torrentListItems(searchResults: SearchResult[], downloads: TorrentState[], view: TorrentView): TorrentListItem[] {
  if (view !== "search") {
    return downloads
      .filter((item) => item.download.status === view)
      .map((item): TorrentListItem => ({ ...item, source: "download" }));
  }
  const downloadsByHash = new Map(downloads.map((item) => [item.hash, item]).filter(([hash]) => Boolean(hash)) as [string, TorrentState][]);
  const currentSearchItems: TorrentListItem[] = [];
  for (const result of searchResults) {
    if (!result.hash) {
      continue;
    }
    const download = downloadsByHash.get(result.hash);
    currentSearchItems.push(download ? { ...download, source: "download" } : searchResultListItem(result));
  }
  return currentSearchItems;
}

function searchResultListItem(result: SearchResult): TorrentListItem {
  const hash = result.hash || "";
  return {
    id: hash || `search:${result.provider}:${result.title}`,
    title: result.title,
    provider: result.provider,
    bytes: result.bytes,
    category: result.category,
    date: result.date,
    seeders: result.seeders,
    peers: result.peers,
    hash,
    magnetUrl: result.magnetUrl,
    downloading: false,
    download: {
      status: "idle",
      completedBytes: 0,
      bytes: result.bytes || 0
    },
    runtime: {
      status: "inactive"
    },
    source: "search",
    searchResult: result
  };
}

function mergeListTorrent(listItem: TorrentState, detailItem?: TorrentState): TorrentState {
  if (!detailItem || detailItem.downloading !== listItem.downloading) {
    return listItem;
  }
  return {
    ...listItem,
    files: detailItem.files,
    runtime: listItem.runtime.status === "inactive" ? detailItem.runtime : listItem.runtime
  };
}

function torrentActionKey(id: string, action: TorrentAction): string {
  return `${id}\0${action}`;
}

function searchResultActionKey(result: SearchResult): string {
  if (result.hash) {
    return searchResultHashActionKey(result.hash);
  }
  return `download\0title:${result.provider}:${result.title}`;
}

function searchResultHashActionKey(hash: string): string {
  return `download\0hash:${hash}`;
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

function torrentListEndpoint(): string {
  return "/api/torrents";
}

function searchEndpoint(): string {
  return "/api/search";
}

function torrentListStreamEndpoint(): string {
  return `${torrentListEndpoint()}/stream`;
}

function torrentEndpoint(id: string): string {
  return `/api/torrents/${encodeURIComponent(id)}`;
}

function torrentActionEndpoint(id: string, action: TorrentAction): string {
  return `${torrentEndpoint(id)}/${action}`;
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

function formatRate(value?: number): string {
  if (value === 0) {
    return "0 B/s";
  }
  const bytes = formatBytes(value);
  return bytes === "-" ? "-" : `${bytes}/s`;
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
