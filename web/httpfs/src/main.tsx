import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import {
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

  return (
    <div className="torrentDetails">
      <section className="detailsSection">
        <div className="sectionHeader">
          <h3>Torrent</h3>
        </div>
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
      </section>
      <RuntimePanel snapshot={runtime.snapshot} />
      <FilePanel item={item} />
    </div>
  );
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
  if (!snapshot) {
    return <div className="panel panelState muted">Runtime pending</div>;
  }

  return (
    <section className="runtimePanel">
      <div className="sectionHeader">
        <h3>Runtime</h3>
        <span>{formatTime(snapshot.updated)}</span>
      </div>
      <RuntimeSummaryGrid summary={snapshot.summary} />
      <PieceMap pieces={snapshot.pieces} />
      <RuntimeDHT servers={snapshot.dht} />
      <RuntimePeers peers={snapshot.peers} />
      <RuntimeEvents events={snapshot.events} />
    </section>
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

function PieceMap({ pieces }: { pieces: RuntimePieceRun[] }) {
  if (pieces.length === 0) {
    return <div className="panel panelState muted">No piece state</div>;
  }
  const total = pieces.reduce((sum, piece) => sum + piece.length, 0);
  return (
    <div className="pieceMap" title={`${total} pieces`}>
      {pieces.map((piece) => (
        <span
          className={`pieceRun ${piece.state}`}
          key={`${piece.start}-${piece.end}-${piece.state}`}
          style={{ flexGrow: Math.max(piece.length, 1) }}
          title={`${piece.start}-${piece.end}: ${piece.state}`}
        />
      ))}
    </div>
  );
}

function RuntimeDHT({ servers }: { servers: RuntimeDHTServer[] }) {
  return (
    <section className="runtimeSection">
      <div className="sectionHeader compact">
        <h3>DHT</h3>
        <span>{servers.length}</span>
      </div>
      {servers.length === 0 ? (
        <div className="panel panelState muted">DHT unavailable</div>
      ) : (
        <div className="compactList">
          {servers.map((server) => (
            <div className="compactRow" key={`${server.id}-${server.address}`}>
              <RadioTower size={15} />
              <span>{server.address}</span>
              <span>{server.goodNodes}/{server.nodes} nodes</span>
              <span>{server.outstandingTransactions} tx</span>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}

function RuntimePeers({ peers }: { peers: RuntimePeer[] }) {
  return (
    <section className="runtimeSection">
      <div className="sectionHeader compact">
        <h3>Peers</h3>
        <span>{peers.length}</span>
      </div>
      {peers.length === 0 ? (
        <div className="panel panelState muted">No peers</div>
      ) : (
        <div className="peerTable">
          {peers.slice(0, 18).map((peer) => (
            <div className="peerRow" key={`${peer.address}-${peer.network}-${peer.connected}`}>
              <Network size={15} />
              <span className="peerAddress">{peer.address}</span>
              <span>{peer.connected ? peer.network || "peer" : "known"}</span>
              <span>{peerSourceLabel(peer.source)}</span>
              <span>{formatBytes(peer.downloadRate)}/s</span>
              <span>{peer.client || "-"}</span>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}

function RuntimeEvents({ events }: { events: RuntimeTorrentEvent[] }) {
  const recent = events.slice(-12).reverse();
  return (
    <section className="runtimeSection">
      <div className="sectionHeader compact">
        <h3>Events</h3>
        <span>{events.length}</span>
      </div>
      {recent.length === 0 ? (
        <div className="panel panelState muted">No events</div>
      ) : (
        <div className="eventList">
          {recent.map((event, index) => (
            <div className="eventRow" key={`${event.time}-${event.type}-${index}`}>
              <span>{formatTime(event.time)}</span>
              <span>{eventLabel(event)}</span>
            </div>
          ))}
        </div>
      )}
    </section>
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
  if (item.status === "unavailable" || item.status === "error") {
    return <div className="panel panelState errorText">{item.error || "Unavailable"}</div>;
  }
  if (item.status === "idle" || item.status === "loading") {
    return <div className="panel panelState muted">Metadata pending</div>;
  }
  if (!item.files || item.files.length === 0) {
    return <div className="panel panelState muted">No files</div>;
  }
  return (
    <section className="assetsPanel">
      <div className="sectionHeader">
        <h3>Files</h3>
        <span>{item.files.length}</span>
      </div>
      <div className="assetList">
        {item.files.map((file) => (
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
    </section>
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
