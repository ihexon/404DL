import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  Activity,
  Calendar,
  Check,
  ChevronDown,
  ChevronRight,
  Database,
  Download,
  FileText,
  HardDrive,
  Network,
  Link,
  Magnet,
  RadioTower,
  Search,
  Users
} from "lucide-react";
import "./styles.css";

type TorrentStatus = "unavailable" | "idle" | "loading" | "ready" | "error";

type FileItem = {
  path: string;
  bytes: number;
  downloadUrl: string;
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
  error?: string;
  files?: FileItem[];
};

type TorrentStore = {
  items: TorrentItem[];
  error: string;
  startMetadata: (item: TorrentItem) => void;
};

type RuntimeSnapshot = {
  id: string;
  updated: string;
  summary: RuntimeSummary;
  peers: RuntimePeer[];
  pieces: RuntimePieceRun[];
  dht: RuntimeDHTServer[];
  events: RuntimeTorrentEvent[];
};

type RuntimeSummary = {
  infoHash?: string;
  name?: string;
  metadataReady: boolean;
  bytesCompleted: number;
  bytesMissing: number;
  length: number;
  totalPeers: number;
  pendingPeers: number;
  activePeers: number;
  connectedSeeders: number;
  halfOpenPeers: number;
  piecesComplete: number;
  numPieces: number;
  chunksReadUseful: number;
  chunksReadWasted: number;
  bytesReadData: number;
  bytesReadUsefulData: number;
  bytesWrittenData: number;
  knownPeers: number;
  activeHalfOpenAttempts: number;
};

type RuntimePeer = {
  address: string;
  source: string;
  network?: string;
  client?: string;
  peerId?: string;
  downloadRate: number;
  uploadRate: number;
  remotePieceCount: number;
  bytesReadData: number;
  bytesReadUsefulData: number;
  bytesWrittenData: number;
  chunksReadUseful: number;
  chunksReadWasted: number;
  connected: boolean;
  supportsEncryption: boolean;
};

type RuntimePieceRun = {
  start: number;
  end: number;
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
const runtimePollIntervalMs = 1200;
const maxPieceMapCells = 320;

const statusLabels: Record<TorrentStatus, string> = {
  unavailable: "Unavailable",
  idle: "Metadata pending",
  loading: "Metadata pending",
  ready: "Files ready",
  error: "Metadata failed"
};

function App() {
  const { items, error, startMetadata } = useTorrents();
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [filter, setFilter] = useState("");

  const visibleItems = useMemo(() => filterTorrents(items, filter), [items, filter]);

  function toggle(item: TorrentItem) {
    const willOpen = !expanded[item.id];
    setExpanded((current) => ({ ...current, [item.id]: willOpen }));
    if (willOpen) {
      startMetadata(item);
    }
  }

  return (
    <main className="shell">
      <header className="topbar">
        <div>
          <h1>mvdl httpfs</h1>
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
        expanded={expanded}
        onToggle={toggle}
      />
    </main>
  );
}

function useTorrents(): TorrentStore {
  const [items, setItems] = useState<TorrentItem[]>([]);
  const [error, setError] = useState("");
  const requestedMetadata = useRef<Set<string>>(new Set());
  const itemsRef = useRef<TorrentItem[]>([]);

  useEffect(() => {
    itemsRef.current = items;
  }, [items]);

  const showServiceError = useCallback((err: unknown) => {
    if (isServiceUnavailable(err)) {
      setError(serviceErrorMessage(err));
    }
  }, []);

  const updateItem = useCallback((next: TorrentItem) => {
    setItems((current) => mergeTorrentLists(current, [next]));
  }, []);

  const loadTorrent = useCallback(
    async (id: string) => {
      try {
        updateItem(await getJSON<TorrentItem>(torrentURL(id)));
      } catch (err) {
        if (err instanceof HTTPError) {
          showServiceError(err);
        }
      }
    },
    [showServiceError, updateItem]
  );

  const startMetadata = useCallback(
    (item: TorrentItem) => {
      if (item.status !== "idle" || requestedMetadata.current.has(item.id)) {
        return;
      }
      requestedMetadata.current.add(item.id);
      setError("");
      setItems((current) => advanceItemStatus(current, item.id, "loading"));
      void getJSON<TorrentItem>(torrentFilesURL(item.id))
        .then(updateItem)
        .catch(showServiceError);
    },
    [showServiceError, updateItem]
  );

  useEffect(() => {
    void getJSON<TorrentItem[]>("/api/torrents")
      .then((next) => setItems((current) => mergeTorrentLists(current, next)))
      .catch((err) => setError(serviceErrorMessage(err)));
  }, []);

  useEffect(() => {
    for (const item of items) {
      startMetadata(item);
    }
  }, [items, startMetadata]);

  useEffect(() => {
    const interval = window.setInterval(() => {
      if (document.hidden) {
        return;
      }
      for (const item of itemsRef.current) {
        if (item.status === "loading") {
          void loadTorrent(item.id);
        }
      }
    }, pollIntervalMs);
    return () => window.clearInterval(interval);
  }, [loadTorrent]);

  return { items, error, startMetadata };
}

function TorrentList({
  items,
  expanded,
  onToggle
}: {
  items: TorrentItem[];
  expanded: Record<string, boolean>;
  onToggle: (item: TorrentItem) => void;
}) {
  return (
    <section className="torrentList" aria-label="Torrents">
      {items.length === 0 && <div className="emptyBox">No torrents</div>}
      {items.map((item) => (
        <TorrentCard
          key={item.id}
          item={item}
          open={Boolean(expanded[item.id])}
          onToggle={() => onToggle(item)}
        />
      ))}
    </section>
  );
}

function TorrentCard({
  item,
  open,
  onToggle
}: {
  item: TorrentItem;
  open: boolean;
  onToggle: () => void;
}) {
  return (
    <article className={`torrentItem ${open ? "open" : ""}`}>
      <div
        className="torrentSummary"
        onClick={onToggle}
        onKeyDown={(event) => handleToggleKeyDown(event, onToggle)}
        role="button"
        tabIndex={0}
      >
        <div className="disclosure" aria-hidden="true">
          {open ? <ChevronDown size={18} /> : <ChevronRight size={18} />}
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

        <StatusBadge status={item.status} />

        <div className="torrentActions" onClick={(event) => event.stopPropagation()}>
          {item.magnetUrl && (
            <CopyButton value={item.magnetUrl} title="Copy MagnetLink" variant="magnet" size={17} />
          )}
        </div>
      </div>

      {open && <TorrentDetails item={item} />}
    </article>
  );
}

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

function TorrentDetails({ item }: { item: TorrentItem }) {
  const runtime = useTorrentRuntime(item);
  const snapshot = runtime.snapshot;

  return (
    <div className="torrentDetails">
      <div className="detailsGrid overviewGrid">
        <TorrentInfoPanel item={item} />
        <RuntimePanel snapshot={snapshot} />
      </div>
      <FilePanel item={item} />
      <div className="detailsGrid runtimeGrid">
        <RuntimePeers snapshot={snapshot} />
        <RuntimeDHT snapshot={snapshot} />
      </div>
      <RuntimeEvents snapshot={snapshot} />
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
  bodyClassName = "",
  children,
  icon,
  meta,
  title
}: {
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
        {meta && <span className="detailBoxMeta">{meta}</span>}
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

function useTorrentRuntime(item: TorrentItem): { snapshot: RuntimeSnapshot | null } {
  const [snapshot, setSnapshot] = useState<RuntimeSnapshot | null>(null);

  useEffect(() => {
    if (item.status === "unavailable" || (!item.hash && !item.magnetUrl)) {
      setSnapshot(null);
      return;
    }

    let canceled = false;

    async function load() {
      try {
        const next = await getJSON<RuntimeSnapshot>(torrentRuntimeURL(item.id));
        if (!canceled) {
          setSnapshot(next);
        }
      } catch {
        if (!canceled) {
          setSnapshot(null);
        }
      }
    }

    void load();
    const interval = window.setInterval(() => {
      if (!document.hidden) {
        void load();
      }
    }, runtimePollIntervalMs);

    return () => {
      canceled = true;
      window.clearInterval(interval);
    };
  }, [item.hash, item.id, item.magnetUrl, item.status]);

  return { snapshot };
}

function RuntimePanel({ snapshot }: { snapshot: RuntimeSnapshot | null }) {
  const pieces = snapshot ? pieceCounts(snapshot.pieces) : null;

  return (
    <DetailBox
      icon={<Activity size={16} />}
      meta={snapshot ? formatTime(snapshot.updated) : "pending"}
      title="Runtime"
    >
      {!snapshot ? (
        <PanelEmpty>Runtime pending</PanelEmpty>
      ) : (
        <div className="runtimePanel">
          <RuntimeSummaryGrid summary={snapshot.summary} />
          <div className="subsection">
            <div className="subsectionHeader">
              <h4>Pieces</h4>
              <span>{pieces ? `${pieces.complete}/${pieces.total}` : "-"}</span>
            </div>
            <PieceMap pieces={snapshot.pieces} />
          </div>
        </div>
      )}
    </DetailBox>
  );
}

function RuntimeSummaryGrid({ summary }: { summary: RuntimeSummary }) {
  const completed = summary.bytesCompleted;
  const total = summary.length || completed + summary.bytesMissing;
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
      <DetailItem label="Active / Known" value={`${summary.activePeers}/${summary.knownPeers}`} />
      <DetailItem label="Pending / Half-open" value={`${summary.pendingPeers}/${summary.halfOpenPeers}`} />
      <DetailItem label="Seeders" value={`${summary.connectedSeeders}`} />
      <DetailItem label="Pieces" value={pieces} />
      <DetailItem label="Useful / Wasted" value={`${summary.chunksReadUseful}/${summary.chunksReadWasted}`} />
      <DetailItem label="Read" value={formatBytes(summary.bytesReadUsefulData)} />
      <DetailItem label="Written" value={formatBytes(summary.bytesWrittenData)} />
    </div>
  );
}

type PieceCell = {
  start: number;
  end: number;
  length: number;
  state: string;
};

function PieceMap({ pieces }: { pieces: RuntimePieceRun[] }) {
  const total = countPieces(pieces);
  if (total === 0) {
    return <PanelEmpty>No piece state</PanelEmpty>;
  }
  const cells = pieceCells(pieces, total);
  const title =
    cells.length === total
      ? `${total} pieces`
      : `${cells.length} blocks representing ${total} pieces`;

  return (
    <div className="pieceMap" title={title}>
      {cells.map((cell) => (
        <span
          className={`pieceCell ${cell.state}`}
          key={`${cell.start}-${cell.end}`}
          title={`${cell.start}-${cell.end}: ${pieceCellLabel(cell.state)} (${cell.length} pieces)`}
        />
      ))}
    </div>
  );
}

function pieceCells(pieces: RuntimePieceRun[], total: number): PieceCell[] {
  const cellCount = Math.min(total, maxPieceMapCells);
  const cells: PieceCell[] = [];
  let runIndex = 0;
  let runStart = 0;

  for (let index = 0; index < cellCount; index++) {
    const start = Math.floor((index * total) / cellCount);
    const end = Math.floor(((index + 1) * total) / cellCount) - 1;
    const length = end - start + 1;
    const scores = new Map<string, number>();

    while (runIndex < pieces.length && runStart + pieces[runIndex].length <= start) {
      runStart += pieces[runIndex].length;
      runIndex++;
    }

    let cursor = start;
    let cursorRunIndex = runIndex;
    let cursorRunStart = runStart;
    while (cursorRunIndex < pieces.length && cursor <= end) {
      const run = pieces[cursorRunIndex];
      const runEnd = cursorRunStart + run.length - 1;
      const overlap = Math.min(end, runEnd) - cursor + 1;
      if (overlap > 0) {
        const state = pieceVisualState(run);
        scores.set(state, (scores.get(state) ?? 0) + overlap);
        cursor += overlap;
      }
      cursorRunStart += run.length;
      cursorRunIndex++;
    }

    cells.push({
      start,
      end,
      length,
      state: dominantPieceState(scores, length)
    });
  }
  return cells;
}

function countPieces(pieces: RuntimePieceRun[]): number {
  return pieces.reduce((sum, piece) => sum + piece.length, 0);
}

function pieceCounts(pieces: RuntimePieceRun[]): { complete: number; total: number } {
  return pieces.reduce(
    (counts, piece) => ({
      complete: counts.complete + (piece.complete ? piece.length : 0),
      total: counts.total + piece.length
    }),
    { complete: 0, total: 0 }
  );
}

function pieceVisualState(piece: RuntimePieceRun): string {
  if (piece.complete) {
    return "complete";
  }
  if (piece.partial || piece.state === "wanted" || piece.priority !== "none") {
    return "active";
  }
  if (piece.hashing || piece.queuedHash) {
    return "hashing";
  }
  return "empty";
}

function dominantPieceState(scores: Map<string, number>, length: number): string {
  if ((scores.get("complete") ?? 0) === length) {
    return "complete";
  }
  if ((scores.get("active") ?? 0) > 0) {
    return "active";
  }
  if ((scores.get("hashing") ?? 0) > 0) {
    return "hashing";
  }
  if ((scores.get("complete") ?? 0) > 0) {
    return "mixed";
  }
  return "empty";
}

function pieceCellLabel(state: string): string {
  switch (state) {
    case "complete":
      return "complete";
    case "active":
      return "downloading";
    case "hashing":
      return "checking";
    case "mixed":
      return "mixed";
    default:
      return "empty";
  }
}

function RuntimeDHT({ snapshot }: { snapshot: RuntimeSnapshot | null }) {
  const servers = snapshot?.dht ?? [];
  return (
    <DetailBox
      bodyClassName="flush"
      icon={<RadioTower size={16} />}
      meta={snapshot ? `${servers.length}` : "pending"}
      title="DHT"
    >
      {!snapshot ? (
        <PanelEmpty>Runtime pending</PanelEmpty>
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

function RuntimePeers({ snapshot }: { snapshot: RuntimeSnapshot | null }) {
  const peers = snapshot?.peers ?? [];
  return (
    <DetailBox
      bodyClassName="flush"
      icon={<Network size={16} />}
      meta={snapshot ? `${peers.length}` : "pending"}
      title="Peers"
    >
      {!snapshot ? (
        <PanelEmpty>Runtime pending</PanelEmpty>
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

function RuntimeEvents({ snapshot }: { snapshot: RuntimeSnapshot | null }) {
  const events = snapshot?.events ?? [];
  const recent = events.slice(-12).reverse();
  return (
    <DetailBox
      bodyClassName="flush"
      icon={<Calendar size={16} />}
      meta={snapshot ? `${events.length}` : "pending"}
      title="Events"
    >
      {!snapshot ? (
        <PanelEmpty>Runtime pending</PanelEmpty>
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

function StatusBadge({ status }: { status: TorrentStatus }) {
  const pending = status === "idle" || status === "loading";
  return (
    <span className={`status ${status}`}>
      {pending && <span className="spinner" aria-hidden="true" />}
      {statusLabels[status]}
    </span>
  );
}

function FilePanel({ item }: { item: TorrentItem }) {
  const files = item.files ?? [];
  const meta = item.status === "ready" ? `${files.length}` : statusLabels[item.status];

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
      <div className="assetList">
        {files.map((file) => (
          <article className="assetRow" key={file.path}>
            <div className="assetIcon" aria-hidden="true">
              <FileText size={18} />
            </div>
            <div className="assetBody">
              <div className="assetName">{file.path}</div>
              <div className="assetMeta">
                <span>{formatBytes(file.bytes)}</span>
              </div>
              <pre className="assetURL">{file.downloadUrl}</pre>
            </div>
            <div className="assetActions">
              <CopyButton value={file.downloadUrl} title="Copy URL" variant="url" size={16} />
              <a
                className="iconButton"
                href={file.downloadUrl}
                rel="noreferrer"
                target="_blank"
                title="Download"
              >
                <Download size={16} />
              </a>
            </div>
          </article>
        ))}
      </div>
    </DetailBox>
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

function filterTorrents(items: TorrentItem[], filter: string): TorrentItem[] {
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

function advanceItemStatus(
  items: TorrentItem[],
  id: string,
  status: TorrentStatus
): TorrentItem[] {
  return items.map((item) => {
    if (item.id !== id || !canMoveStatus(item.status, status)) {
      return item;
    }
    return { ...item, status };
  });
}

function mergeTorrentLists(current: TorrentItem[], next: TorrentItem[]): TorrentItem[] {
  if (current.length === 0) {
    return next;
  }

  const nextByID = new Map(next.map((item) => [item.id, item]));
  return current
    .map((item) => {
      const replacement = nextByID.get(item.id);
      if (!replacement) {
        return item;
      }
      nextByID.delete(item.id);
      return mergeTorrentItem(item, replacement);
    })
    .concat([...nextByID.values()]);
}

function mergeTorrentItem(current: TorrentItem, next: TorrentItem): TorrentItem {
  if (!canMoveStatus(current.status, next.status)) {
    return current;
  }
  return next;
}

function canMoveStatus(current: TorrentStatus, next: TorrentStatus): boolean {
  return current === next || statusRank(next) > statusRank(current);
}

function statusRank(status: TorrentStatus): number {
  switch (status) {
    case "idle":
      return 0;
    case "loading":
      return 1;
    case "ready":
    case "error":
    case "unavailable":
      return 2;
  }
}

function torrentURL(id: string): string {
  return `/api/torrents/${encodeURIComponent(id)}`;
}

function torrentFilesURL(id: string): string {
  return `${torrentURL(id)}/files`;
}

function torrentRuntimeURL(id: string): string {
  return `${torrentURL(id)}/runtime`;
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

async function copyText(value: string) {
  await navigator.clipboard.writeText(value);
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

function isServiceUnavailable(err: unknown): boolean {
  if (err instanceof HTTPError) {
    return err.status >= 500;
  }
  return err instanceof Error && err.message.startsWith("HTTP service unavailable:");
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
