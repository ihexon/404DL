import React, { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  Activity,
  Calendar,
  Check,
  ChevronRight,
  CircleStop,
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
  Settings,
  Trash2,
  Upload,
  Users
} from "lucide-react";
import "./styles.css";

type FileStatus = "idle" | "downloading" | "complete";
type TaskStatus = "idle" | "downloading" | "paused" | "complete";

type FileItem = {
  path: string;
  bytes: number;
  completedBytes: number;
  savePath: string;
  status: FileStatus;
};

type TaskItem = {
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
  path: string;
  downloading: boolean;
  uploading: boolean;
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
  downloadDir: string;
  searchResults: SearchResult[];
  tasks: TaskState[];
};

type DownloadView = {
  status: TaskStatus;
  completedBytes: number;
  bytes: number;
};

type TaskState = TaskItem & {
  runtime: RuntimeView;
};

type SearchEntry =
  | { kind: "task"; task: TaskState }
  | { kind: "candidate"; result: SearchResult };

type TaskHeaderData = {
  title: string;
  provider: string;
  bytes?: number;
  seeders: number;
  peers: number;
  date?: string;
  category?: string;
  hash?: string;
  magnetUrl?: string;
  badges: TaskBadge[];
};

type TaskBadge = "downloading" | "paused" | "complete" | "seeding";
type SeedingState = "unavailable" | "stopped" | "seeding";

type RuntimeView = {
  status: "inactive" | "ready" | "error";
  snapshot?: RuntimeSnapshot;
  error?: string;
};

type TaskStore = {
  downloadDir: string;
  downloads: TaskState[];
  error: string;
  inFlightCommands: Set<string>;
  searchResults: SearchResult[];
  createMagnetTask: (magnetUrl: string, options?: CreateTaskOptions) => Promise<TaskState | null>;
  createSearchResultTask: (result: SearchResult, options?: CreateTaskOptions) => Promise<void>;
  loadTask: (id: string) => Promise<void>;
  runTaskAction: (item: TaskState, action: TaskAction, options?: TaskActionOptions) => Promise<void>;
  search: (query: string, limit: number) => Promise<void>;
};

type RuntimeSnapshot = {
  id: string;
  updated: string;
  summary: RuntimeSummary;
  peers: RuntimePeer[];
  pieceRuns: RuntimePieceRun[];
  dht: RuntimeDHTServer[];
  events: RuntimeTaskEvent[];
};

type RuntimeSummary = {
  infoHash?: string;
  name?: string;
  metadataReady: boolean;
  bytesCompleted: number;
  length: number;
  transfer: TransferStats;
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

type TransferStats = {
  downloadRate: number;
  uploadRate: number;
};

type RuntimePeer = {
  address: string;
  source: string;
  network?: string;
  client?: string;
  transfer: TransferStats;
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

type RuntimeTaskEvent = {
  time: string;
  type: string;
  infoHash?: string;
  peer?: string;
  source?: string;
  network?: string;
  client?: string;
  message?: string;
  error?: string;
  dhtQuery?: string;
  dhtNode?: string;
};

type TaskAction = "continue" | "pause" | "start-seeding" | "stop-seeding" | "delete";
type TaskView = "search" | "active" | "done" | "settings";
type AppSettings = {
  newTaskPath: string;
};
type CreateTaskOptions = {
  path?: string;
};
type TaskActionOptions = {
  force?: boolean;
};

const deleteKeepFilesKey = "4dl.delete.keepFiles";
const settingsKey = "4dl.settings";

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
    downloadDir,
    downloads,
    createMagnetTask,
    createSearchResultTask,
    error,
    inFlightCommands,
    loadTask,
    runTaskAction,
    search,
    searchResults
  } = useTasks();
  const [expandedEntryKeys, setExpandedEntryKeys] = useState<Record<string, boolean>>({});
  const [view, setView] = useState<TaskView>("active");
  const [settings, setSettings] = useState<AppSettings>(loadSettings);
  const [taskPendingDelete, setTaskPendingDelete] = useState<TaskState | null>(null);
  const [keepFilesOnDelete, setKeepFilesOnDelete] = useState(() => localStorage.getItem(deleteKeepFilesKey) !== "false");

  const visibleTasks = useMemo(() => visibleTasksForView(downloads, view), [downloads, view]);
  const searchEntries = useMemo(() => buildSearchEntries(searchResults, downloads), [searchResults, downloads]);
  const activeCount = useMemo(() => downloads.filter(isActiveTask).length, [downloads]);
  const doneCount = downloads.length - activeCount;
  const hasSearchResults = searchResults.length > 0;
  const createOptions = useMemo<CreateTaskOptions>(
    () => ({ path: settings.newTaskPath.trim() }),
    [settings.newTaskPath]
  );

  function toggleTaskDetails(item: TaskState) {
    const key = taskEntryKey(item);
    const willOpen = !expandedEntryKeys[key];
    toggleEntry(key);
    if (!willOpen) {
      return;
    }
    void loadTask(item.id);
  }

  function toggleEntry(key: string) {
    setExpandedEntryKeys((current) => ({ ...current, [key]: !current[key] }));
  }

  async function runTaskEntryAction(item: TaskState, action: TaskAction) {
    if (action === "delete") {
      setTaskPendingDelete(item);
      return;
    }
    await runTaskAction(item, action);
  }

  async function confirmDelete() {
    if (!taskPendingDelete) {
      return;
    }
    localStorage.setItem(deleteKeepFilesKey, keepFilesOnDelete ? "true" : "false");
    const item = taskPendingDelete;
    setTaskPendingDelete(null);
    await runTaskAction(item, "delete", { force: !keepFilesOnDelete });
  }

  function changeSettings(next: AppSettings) {
    setSettings(next);
    saveSettings(next);
  }

  return (
    <main className="shell">
      <header className="topbar">
        <div>
          <h1>4dl</h1>
          <div className="meta">{formatCount(activeCount, "active")} · {formatCount(doneCount, "done")}</div>
        </div>
      </header>

      {error && <div className="banner">{error}</div>}

      <CreatePanel
        createMagnetTask={(magnetUrl) => createMagnetTask(magnetUrl, createOptions)}
        onTaskCreated={(task) => {
          setView("active");
          setExpandedEntryKeys((current) => ({ ...current, [taskEntryKey(task)]: true }));
          void loadTask(task.id);
        }}
        onSearchComplete={() => setView("search")}
        search={search}
        searchResults={searchResults}
      />

      <div className="listToolbar">
        <div className="viewTabs" aria-label="Task view">
          <ViewTab current={view} disabled={!hasSearchResults} onChange={setView} value="search">Search</ViewTab>
          <ViewTab current={view} onChange={setView} value="active">Active</ViewTab>
          <ViewTab current={view} onChange={setView} value="done">Done</ViewTab>
          <ViewTab current={view} onChange={setView} value="settings">Settings</ViewTab>
        </div>
      </div>

      {view === "search" ? (
        <SearchView
          expandedEntryKeys={expandedEntryKeys}
          inFlightCommands={inFlightCommands}
          onCreate={(result) => createSearchResultTask(result, createOptions)}
          onTaskAction={runTaskEntryAction}
          onToggle={toggleTaskDetails}
          onToggleEntry={toggleEntry}
          entries={searchEntries}
        />
      ) : view === "settings" ? (
        <SettingsPanel
          currentDownloadDir={downloadDir}
          settings={settings}
          onChange={changeSettings}
        />
      ) : (
        <TaskEntries
          tasks={visibleTasks}
          inFlightCommands={inFlightCommands}
          expandedEntryKeys={expandedEntryKeys}
          onTaskAction={runTaskEntryAction}
          onToggle={toggleTaskDetails}
        />
      )}

      {taskPendingDelete && (
        <DeleteTaskDialog
          task={taskPendingDelete}
          keepFiles={keepFilesOnDelete}
          onCancel={() => setTaskPendingDelete(null)}
          onChangeKeepFiles={setKeepFilesOnDelete}
          onConfirm={confirmDelete}
        />
      )}
    </main>
  );
}

function useTasks(): TaskStore {
  const [state, setState] = useState<AppState | null>(null);
  const [detailsByID, setDetailsByID] = useState<Map<string, TaskState>>(() => new Map());
  const [error, setError] = useState("");
  const [inFlightCommands, setInFlightCommands] = useState<Set<string>>(new Set());
  const inFlightCommandsRef = useRef<Set<string>>(new Set());
  const detailsRef = useRef<Map<string, TaskState>>(new Map());

  useEffect(() => {
    detailsRef.current = detailsByID;
  }, [detailsByID]);

  const mergeAppState = useCallback((next: AppState) => {
    const details = detailsRef.current;
    next.tasks = next.tasks.map((item) => mergeListTask(item, details.get(item.id)));
    setState(next);
  }, []);

  const mergeTask = useCallback((next: TaskState) => {
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
        tasks: current.tasks.some((item) => item.id === next.id)
          ? current.tasks.map((item) => item.id === next.id ? next : item)
          : [next, ...current.tasks]
      };
    });
  }, []);

  const loadTask = useCallback(
    async (id: string) => {
      const next = await getJSON<TaskState>(taskEndpoint(id));
      mergeTask(next);
    },
    [mergeTask]
  );

  const showServiceError = useCallback((err: unknown) => {
    setError(serviceErrorMessage(err));
  }, []);

  const runTaskAction = useCallback(
    async (item: TaskState, action: TaskAction, options: TaskActionOptions = {}) => {
      const commandKey = taskActionKey(item.id, action);
      if (inFlightCommandsRef.current.has(commandKey)) {
        return;
      }
      addInFlightCommand(inFlightCommandsRef.current, setInFlightCommands, commandKey);
      setError("");
      try {
        const next = await requestJSON<TaskState>(taskActionEndpoint(item.id, action, options), taskActionMethod(action));
        mergeTask(next);
      } catch (err) {
        showServiceError(err);
      } finally {
        clearInFlightCommand(inFlightCommandsRef.current, setInFlightCommands, commandKey);
      }
    },
    [mergeTask, showServiceError]
  );

  const createSearchResultTask = useCallback(
    async (result: SearchResult, options: CreateTaskOptions = {}): Promise<void> => {
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
        const item = await postJSON<TaskState>(taskListEndpoint(), createTaskPayload({ result }, options));
        mergeTask(item);
        const next = await requestJSON<TaskState>(taskActionEndpoint(item.id, "continue"), "PUT");
        mergeTask(next);
      } catch (err) {
        showServiceError(err);
      } finally {
        clearInFlightCommand(inFlightCommandsRef.current, setInFlightCommands, commandKey);
      }
    },
    [mergeTask, showServiceError]
  );

  const createMagnetTask = useCallback(
    async (magnetUrl: string, options: CreateTaskOptions = {}): Promise<TaskState | null> => {
      const commandKey = `magnet\0${magnetUrl}`;
      if (inFlightCommandsRef.current.has(commandKey)) {
        return null;
      }
      addInFlightCommand(inFlightCommandsRef.current, setInFlightCommands, commandKey);
      setError("");
      try {
        const item = await postJSON<TaskState>(taskListEndpoint(), createTaskPayload({ magnetUrl }, options));
        mergeTask(item);
        const next = await requestJSON<TaskState>(taskActionEndpoint(item.id, "continue"), "PUT");
        mergeTask(next);
        return next;
      } catch (err) {
        showServiceError(err);
        return null;
      } finally {
        clearInFlightCommand(inFlightCommandsRef.current, setInFlightCommands, commandKey);
      }
    },
    [mergeTask, showServiceError]
  );

  const search = useCallback(
    async (query: string, limit: number): Promise<void> => {
      setError("");
      try {
        await postJSON<SearchResult[]>(searchEndpoint(), { query, limit });
      } catch (err) {
        showServiceError(err);
      }
    },
    [showServiceError]
  );

  useEffect(() => {
    const source = new EventSource(taskListStreamEndpoint());
    source.addEventListener("open", () => setError(""));
    source.addEventListener("state", (event) => {
      mergeAppState(JSON.parse(event.data) as AppState);
    });
    return () => source.close();
  }, [mergeAppState]);

  return {
    downloadDir: state?.downloadDir ?? "",
    downloads: state?.tasks ?? [],
    error,
    inFlightCommands,
    searchResults: state?.searchResults ?? [],
    createMagnetTask,
    createSearchResultTask,
    loadTask,
    runTaskAction,
    search
  };
}

function CreatePanel({
  createMagnetTask,
  onSearchComplete,
  onTaskCreated,
  search,
  searchResults
}: {
  createMagnetTask: (magnetUrl: string) => Promise<TaskState | null>;
  onSearchComplete: () => void;
  onTaskCreated: (task: TaskState) => void;
  search: (query: string, limit: number) => Promise<void>;
  searchResults: SearchResult[];
}) {
  const [query, setQuery] = useState("");
  const [limit, setLimit] = useState(50);
  const [searching, setSearching] = useState(false);

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const normalized = query.trim();
    if (!normalized || searching) {
      return;
    }
    setSearching(true);
    try {
      if (isMagnetInput(normalized)) {
        const task = await createMagnetTask(normalized);
        if (task) {
          onTaskCreated(task);
          setQuery("");
        }
        return;
      }
      await search(normalized, limit);
      onSearchComplete();
    } finally {
      setSearching(false);
    }
  }

  return (
    <section className="createPanel">
      <form className="createForm" onSubmit={(event) => { void submit(event); }}>
        <label className="queryField">
          <Search size={16} />
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Search torrents or paste MagnetLink"
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
          <span>{isMagnetInput(query) ? "Create" : "Search"}</span>
        </button>
      </form>
      {searchResults.length > 0 && (
        <div className="createPanelMeta">
          {formatCount(searchResults.length, "result")}
        </div>
      )}
    </section>
  );
}

function SearchView({
  expandedEntryKeys,
  entries,
  inFlightCommands,
  onCreate,
  onTaskAction,
  onToggle,
  onToggleEntry
}: {
  expandedEntryKeys: Record<string, boolean>;
  entries: SearchEntry[];
  inFlightCommands: Set<string>;
  onCreate: (result: SearchResult) => Promise<void>;
  onTaskAction: (item: TaskState, action: TaskAction) => Promise<void>;
  onToggle: (item: TaskState) => void;
  onToggleEntry: (key: string) => void;
}) {
  return (
    <section className="taskList" aria-label="Search results">
      {entries.length === 0 ? (
        <div className="emptyBox">No search results</div>
      ) : (
        <SearchEntries
          expandedEntryKeys={expandedEntryKeys}
          entries={entries}
          inFlightCommands={inFlightCommands}
          onCreate={onCreate}
          onTaskAction={onTaskAction}
          onToggle={onToggle}
          onToggleEntry={onToggleEntry}
        />
      )}
    </section>
  );
}

function SearchEntries({
  expandedEntryKeys,
  entries,
  inFlightCommands,
  onCreate,
  onTaskAction,
  onToggle,
  onToggleEntry
}: {
  expandedEntryKeys: Record<string, boolean>;
  entries: SearchEntry[];
  inFlightCommands: Set<string>;
  onCreate: (result: SearchResult) => Promise<void>;
  onTaskAction: (item: TaskState, action: TaskAction) => Promise<void>;
  onToggle: (item: TaskState) => void;
  onToggleEntry: (key: string) => void;
}) {
  return (
    <>
      {entries.map((entry, index) => {
        if (entry.kind === "task") {
          return (
            <TaskEntry
              key={`${taskEntryKey(entry.task)}:${index}`}
              task={entry.task}
              inFlightCommands={inFlightCommands}
              open={Boolean(expandedEntryKeys[taskEntryKey(entry.task)])}
              onTaskAction={onTaskAction}
              onToggle={() => onToggle(entry.task)}
            />
          );
        }
        const key = candidateTaskKey(entry.result);
        return (
          <CandidateTaskEntry
            key={`${key}:${index}`}
            inFlightCommands={inFlightCommands}
            onCreate={onCreate}
            onToggle={() => onToggleEntry(key)}
            open={Boolean(expandedEntryKeys[key])}
            result={entry.result}
          />
        );
      })}
    </>
  );
}

function CandidateTaskEntry({
  inFlightCommands,
  onCreate,
  onToggle,
  open,
  result
}: {
  inFlightCommands: Set<string>;
  onCreate: (result: SearchResult) => Promise<void>;
  onToggle: () => void;
  open: boolean;
  result: SearchResult;
}) {
  const busy = inFlightCommands.has(searchResultActionKey(result));
  return (
    <article className="taskItem">
      <TaskHeader
        actions={(
          <>
            <button
              className="iconButton taskAction continue"
              disabled={busy || !result.hash}
              onClick={() => { void onCreate(result); }}
              title="Create and continue"
            >
              {busy ? <span className="spinner" aria-hidden="true" /> : <Play size={16} />}
            </button>
            {result.magnetUrl && (
              <CopyButton value={result.magnetUrl} title="Copy MagnetLink" variant="magnet" size={17} />
            )}
          </>
        )}
        data={taskHeaderDataFromSearchResult(result)}
        open={open}
        onToggle={onToggle}
      />
      {open && <CandidateTaskDetails result={result} />}
    </article>
  );
}

function CandidateTaskDetails({ result }: { result: SearchResult }) {
  return (
    <div className="taskDetails">
      <DetailBox icon={<Database size={16} />} title="Torrent">
        <div className="taskInfoBody">
          <dl className="detailGrid">
            <DetailItem label="Provider" value={result.provider || "-"} />
            <DetailItem label="Size" value={formatBytes(result.bytes)} />
            <DetailItem label="Date" value={formatDate(result.date)} />
            <DetailItem label="Seeds / Peers" value={`${result.seeders}/${result.peers}`} />
            <DetailItem label="Category" value={result.category || "-"} />
          </dl>
          <CodeBlock label="Info Hash" value={result.hash || ""} />
          <CodeBlock
            action={
              result.magnetUrl ? (
                <CopyButton value={result.magnetUrl} title="Copy MagnetLink" variant="magnet" size={15} />
              ) : null
            }
            label="MagnetLink"
            value={result.magnetUrl || ""}
          />
        </div>
      </DetailBox>
    </div>
  );
}

function SettingsPanel({
  currentDownloadDir,
  settings,
  onChange
}: {
  currentDownloadDir: string;
  settings: AppSettings;
  onChange: (settings: AppSettings) => void;
}) {
  const newTaskPath = settings.newTaskPath.trim();
  return (
    <section className="settingsPanel" aria-label="Settings">
      <DetailBox icon={<Settings size={16} />} title="New Tasks">
        <div className="settingsBody">
          <label className="settingsField">
            <span className="settingsLabel">Default save path</span>
            <input
              value={settings.newTaskPath}
              onChange={(event) => onChange({ ...settings, newTaskPath: event.target.value })}
              placeholder={currentDownloadDir || "Server default download directory"}
            />
          </label>
          <div className="settingsNote">
            {newTaskPath
              ? `New tasks will be created under ${newTaskPath}.`
              : currentDownloadDir
                ? `New tasks use the server default: ${currentDownloadDir}.`
                : "New tasks use the server default download directory."}
          </div>
          <div className="settingsNote">
            Existing tasks keep their original paths.
          </div>
        </div>
      </DetailBox>
    </section>
  );
}

function TaskEntries({
  tasks,
  inFlightCommands,
  expandedEntryKeys,
  onTaskAction,
  onToggle
}: {
  tasks: TaskState[];
  inFlightCommands: Set<string>;
  expandedEntryKeys: Record<string, boolean>;
  onTaskAction: (item: TaskState, action: TaskAction) => Promise<void>;
  onToggle: (item: TaskState) => void;
}) {
  return (
    <section className="taskList" aria-label="Tasks">
      {tasks.length === 0 && <div className="emptyBox">No tasks</div>}
      {tasks.map((task) => (
        <TaskEntry
          key={taskEntryKey(task)}
          task={task}
          inFlightCommands={inFlightCommands}
          open={Boolean(expandedEntryKeys[taskEntryKey(task)])}
          onTaskAction={onTaskAction}
          onToggle={() => onToggle(task)}
        />
      ))}
    </section>
  );
}

const TaskEntry = React.memo(function TaskEntry({
  task,
  inFlightCommands,
  open,
  onTaskAction,
  onToggle
}: {
  task: TaskState;
  inFlightCommands: Set<string>;
  open: boolean;
  onTaskAction: (item: TaskState, action: TaskAction) => Promise<void>;
  onToggle: () => void;
}) {
  return (
    <article className={`taskItem ${open ? "open" : ""} ${task.downloading ? "downloading" : ""}`}>
      <TaskHeader
        actions={(
          <>
            {taskSeedingState(task) !== "unavailable" ? (
              <SeedingToggleButton
                inFlightCommands={inFlightCommands}
                task={task}
                onAction={onTaskAction}
              />
            ) : (
              <DownloadToggleButton
                inFlightCommands={inFlightCommands}
                task={task}
                onAction={onTaskAction}
              />
            )}
            <TaskActionButton
              action="delete"
              icon={<Trash2 size={16} />}
              inFlightCommands={inFlightCommands}
              task={task}
              onAction={onTaskAction}
              title="Delete task"
            />
            {task.magnetUrl && (
              <CopyButton value={task.magnetUrl} title="Copy MagnetLink" variant="magnet" size={17} />
            )}
          </>
        )}
        data={taskHeaderDataFromTask(task)}
        open={open}
        onToggle={onToggle}
      />

      {open && (
        <TaskDetails
          item={task}
        />
      )}
    </article>
  );
});

function TaskHeader({
  actions,
  data,
  open,
  onToggle
}: {
  actions: React.ReactNode;
  data: TaskHeaderData;
  open?: boolean;
  onToggle?: () => void;
}) {
  const canToggle = Boolean(onToggle);
  return (
    <div
      className={`taskHeader ${canToggle ? "toggleable" : ""}`}
      onClick={onToggle}
      onKeyDown={onToggle ? (event) => handleToggleKeyDown(event, onToggle) : undefined}
      role={canToggle ? "button" : undefined}
      tabIndex={canToggle ? 0 : undefined}
    >
      <div className={`disclosure ${open ? "open" : ""} ${canToggle ? "" : "hidden"}`} aria-hidden="true">
        {canToggle && <ChevronRight size={18} />}
      </div>

      <div className="taskBody">
        <h2>{data.title || "Untitled"}</h2>
        <div className="metaPills">
          <MetaPill icon={<Database size={14} />} value={data.provider || "-"} />
          <MetaPill icon={<HardDrive size={14} />} value={formatBytes(data.bytes)} />
          <MetaPill icon={<Users size={14} />} value={`${data.seeders}/${data.peers}`} />
          <MetaPill icon={<Calendar size={14} />} value={formatDate(data.date)} />
          {data.category && <MetaPill value={data.category} />}
          {data.hash && <MetaPill value={shortHash(data.hash)} />}
        </div>
      </div>

      <TaskStatusBadges badges={data.badges} />

      <div className="taskActions" onClick={(event) => event.stopPropagation()}>
        {actions}
      </div>
    </div>
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

function TaskDetails({ item }: { item: TaskState }) {
  return (
    <div className="taskDetails">
      <div className="detailsGrid overviewGrid">
        <TaskInfoPanel item={item} />
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

function TaskInfoPanel({ item }: { item: TaskItem }) {
  return (
    <DetailBox
      icon={<Database size={16} />}
      title="Task"
    >
      <div className="taskInfoBody">
        <dl className="detailGrid">
          <DetailItem label="Provider" value={item.provider || "-"} />
          <DetailItem label="Size" value={formatBytes(item.bytes)} />
          <DetailItem label="Date" value={formatDate(item.date)} />
          <DetailItem label="Seeds / Peers" value={`${item.seeders}/${item.peers}`} />
          <DetailItem label="Category" value={item.category || "-"} />
        </dl>
        <CodeBlock label="Path" value={item.path || ""} />
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
      <DetailItem label="Download speed" value={formatRate(summary.transfer.downloadRate)} />
      <DetailItem label="Upload speed" value={formatRate(summary.transfer.uploadRate)} />
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
              <span>{formatRate(peer.transfer.downloadRate)}</span>
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
  disabled = false,
  onChange,
  value
}: {
  children: React.ReactNode;
  current: TaskView;
  disabled?: boolean;
  onChange: (value: TaskView) => void;
  value: TaskView;
}) {
  return (
    <button
      className={`viewTab ${current === value ? "active" : ""}`}
      disabled={disabled}
      onClick={() => onChange(value)}
      type="button"
    >
      {children}
    </button>
  );
}

function TaskStatusBadges({ badges }: { badges: TaskBadge[] }) {
  if (badges.length === 0) {
    return null;
  }
  return (
    <div className="statusBadges">
      {badges.map((badge) => <TaskStatusBadge key={badge} badge={badge} />)}
    </div>
  );
}

function TaskStatusBadge({ badge }: { badge: TaskBadge }) {
  if (badge === "downloading") {
    return (
      <span className="status downloading">
        <span className="spinner" aria-hidden="true" />
        Downloading
      </span>
    );
  }
  if (badge === "paused") {
    return (
      <span className="status paused">
        <Pause size={14} />
        Paused
      </span>
    );
  }
  if (badge === "seeding") {
    return (
      <span className="status seeding">
        <Upload size={14} />
        Seeding
      </span>
    );
  }
  return (
    <span className="status complete">
      <Check size={14} />
      Complete
    </span>
  );
}

function DownloadToggleButton({
  inFlightCommands,
  task,
  onAction
}: {
  inFlightCommands: Set<string>;
  task: TaskState;
  onAction: (item: TaskState, action: TaskAction) => Promise<void>;
}) {
  const action = taskDownloadAction(task);
  if (!action) {
    return null;
  }
  return (
    <TaskActionButton
      action={action}
      icon={action === "pause" ? <Pause size={16} /> : <Download size={16} />}
      inFlightCommands={inFlightCommands}
      task={task}
      onAction={onAction}
      title={action === "pause" ? "Pause download" : "Continue"}
    />
  );
}

function SeedingToggleButton({
  inFlightCommands,
  task,
  onAction
}: {
  inFlightCommands: Set<string>;
  task: TaskState;
  onAction: (item: TaskState, action: TaskAction) => Promise<void>;
}) {
  const action = taskSeedingAction(task);
  if (!action) {
    return null;
  }
  const seeding = action === "stop-seeding";
  return (
    <TaskActionButton
      action={action}
      icon={seeding ? <CircleStop size={16} /> : <Upload size={16} />}
      inFlightCommands={inFlightCommands}
      task={task}
      onAction={onAction}
      title={seeding ? "Stop seeding" : "Start seeding"}
    />
  );
}

function TaskActionButton({
  action,
  icon,
  inFlightCommands,
  task,
  onAction,
  title
}: {
  action: TaskAction;
  icon: React.ReactNode;
  inFlightCommands: Set<string>;
  task: TaskState;
  onAction: (item: TaskState, action: TaskAction) => Promise<void>;
  title: string;
}) {
  const commandKey = taskActionKey(task.id, action);
  const searchCommandKey = task.hash ? searchResultHashActionKey(task.hash) : "";
  const busy = inFlightCommands.has(commandKey) || (action === "continue" && searchCommandKey !== "" && inFlightCommands.has(searchCommandKey));
  const disabled = busy || !canRunTaskAction(task, action);
  return (
    <button
      className={`iconButton taskAction ${action}`}
      disabled={disabled}
      onClick={() => { void onAction(task, action); }}
      title={title}
    >
      {busy ? <span className="spinner" aria-hidden="true" /> : icon}
    </button>
  );
}

function DeleteTaskDialog({
  task,
  keepFiles,
  onCancel,
  onChangeKeepFiles,
  onConfirm
}: {
  task: TaskState;
  keepFiles: boolean;
  onCancel: () => void;
  onChangeKeepFiles: (value: boolean) => void;
  onConfirm: () => Promise<void>;
}) {
  return (
    <div className="modalBackdrop" role="presentation" onMouseDown={onCancel}>
      <section
        aria-labelledby="deleteTaskTitle"
        aria-modal="true"
        className="modalPanel"
        role="dialog"
        onMouseDown={(event) => event.stopPropagation()}
      >
        <div className="modalHeader">
          <Trash2 size={18} />
          <h2 id="deleteTaskTitle">Delete task</h2>
        </div>
        <div className="deleteTaskName">{task.title}</div>
        <label className="checkboxRow">
          <input
            checked={keepFiles}
            onChange={(event) => onChangeKeepFiles(event.currentTarget.checked)}
            type="checkbox"
          />
          <span>Keep downloaded files</span>
        </label>
        <div className="modalActions">
          <button className="secondaryButton" onClick={onCancel} type="button">Cancel</button>
          <button className="dangerButton" onClick={() => { void onConfirm(); }} type="button">
            {keepFiles ? "Delete task" : "Delete task and files"}
          </button>
        </div>
      </section>
    </div>
  );
}

function canRunTaskAction(task: TaskState, action: TaskAction): boolean {
  if (!task.hash) {
    return false;
  }
  switch (action) {
    case "continue":
      return taskDownloadAction(task) === "continue";
    case "pause":
      return taskDownloadAction(task) === "pause";
    case "start-seeding":
      return taskSeedingAction(task) === "start-seeding";
    case "stop-seeding":
      return taskSeedingAction(task) === "stop-seeding";
    case "delete":
      return true;
  }
}

function taskDownloadAction(task: TaskState): Extract<TaskAction, "continue" | "pause"> | null {
  if (task.download.status === "downloading") {
    return "pause";
  }
  if (task.download.status === "complete") {
    return null;
  }
  return "continue";
}

function taskSeedingAction(task: TaskState): Extract<TaskAction, "start-seeding" | "stop-seeding"> | null {
  const state = taskSeedingState(task);
  if (state === "unavailable") {
    return null;
  }
  return state === "seeding" ? "stop-seeding" : "start-seeding";
}

function taskSeedingState(task: TaskState): SeedingState {
  if (task.download.status !== "complete") {
    return "unavailable";
  }
  return task.runtime.status === "ready" ? "seeding" : "stopped";
}

function taskStatusBadges(task: TaskState): TaskBadge[] {
  if (task.download.status === "downloading") {
    return ["downloading"];
  }
  if (task.download.status === "paused") {
    return ["paused"];
  }
  if (task.download.status !== "complete") {
    return [];
  }
  return taskSeedingState(task) === "seeding" ? ["complete", "seeding"] : ["complete"];
}

function FilePanel({ item }: { item: TaskState }) {
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

function visibleTasksForView(downloads: TaskState[], view: TaskView): TaskState[] {
  if (view === "search" || view === "settings") {
    return [];
  }
  return downloads.filter((item) => view === "done" ? item.download.status === "complete" : isActiveTask(item));
}

function buildSearchEntries(results: SearchResult[], tasks: TaskState[]): SearchEntry[] {
  const tasksByHash = new Map<string, TaskState>();
  for (const task of tasks) {
    const key = task.hash ? hashKey(task.hash) : "";
    if (key) {
      tasksByHash.set(key, task);
    }
  }
  return results.map((result) => {
    const task = result.hash ? tasksByHash.get(hashKey(result.hash)) : undefined;
    return task ? { kind: "task", task } : { kind: "candidate", result };
  });
}

function taskHeaderDataFromTask(task: TaskState): TaskHeaderData {
  return {
    title: task.title,
    provider: task.provider,
    bytes: task.bytes,
    seeders: task.seeders,
    peers: task.peers,
    date: task.date,
    category: task.category,
    hash: task.hash,
    magnetUrl: task.magnetUrl,
    badges: taskStatusBadges(task)
  };
}

function taskHeaderDataFromSearchResult(result: SearchResult): TaskHeaderData {
  return {
    title: result.title,
    provider: result.provider,
    bytes: result.bytes,
    seeders: result.seeders,
    peers: result.peers,
    date: result.date,
    category: result.category,
    hash: result.hash,
    magnetUrl: result.magnetUrl,
    badges: []
  };
}

function hashKey(hash: string): string {
  return hash.trim().toLowerCase();
}

function shortHash(hash: string): string {
  return hash.trim().slice(0, 12);
}

function isActiveTask(item: TaskState): boolean {
  return item.download.status !== "complete";
}

function mergeListTask(listItem: TaskState, detailItem?: TaskState): TaskState {
  if (!detailItem || detailItem.downloading !== listItem.downloading) {
    return listItem;
  }
  return {
    ...listItem,
    files: detailItem.files,
    runtime: listItem.runtime.status === "inactive" ? detailItem.runtime : listItem.runtime
  };
}

function taskActionKey(id: string, action: TaskAction): string {
  return `${id}\0${action}`;
}

function taskEntryKey(task: TaskState): string {
  return `task:${task.id}`;
}

function searchResultActionKey(result: SearchResult): string {
  if (result.hash) {
    return searchResultHashActionKey(result.hash);
  }
  return `download\0title:${result.provider}:${result.title}`;
}

function candidateTaskKey(result: SearchResult): string {
  if (result.hash) {
    return `candidate:${hashKey(result.hash)}`;
  }
  return `candidate:${result.provider}:${result.title}:${result.magnetUrl || ""}`;
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

function taskListEndpoint(): string {
  return "/api/tasks";
}

function searchEndpoint(): string {
  return "/api/search";
}

function taskListStreamEndpoint(): string {
  return `${taskListEndpoint()}/stream`;
}

function taskEndpoint(id: string): string {
  return `/api/tasks/${encodeURIComponent(id)}`;
}

function taskActionEndpoint(id: string, action: TaskAction, options: TaskActionOptions = {}): string {
  if (action === "delete") {
    const endpoint = taskEndpoint(id);
    if (options.force === undefined) {
      return endpoint;
    }
    return `${endpoint}?force=${options.force ? "true" : "false"}`;
  }
  return `${taskEndpoint(id)}/${action}`;
}

function taskActionMethod(action: TaskAction): "PUT" | "DELETE" {
  return action === "delete" ? "DELETE" : "PUT";
}

function isMagnetInput(value: string): boolean {
  return value.trim().toLowerCase().startsWith("magnet:");
}

function createTaskPayload<T extends Record<string, unknown>>(base: T, options: CreateTaskOptions): T & CreateTaskOptions {
  const path = options.path?.trim();
  if (!path) {
    return base;
  }
  return { ...base, path };
}

function loadSettings(): AppSettings {
  const fallback: AppSettings = { newTaskPath: "" };
  const raw = localStorage.getItem(settingsKey);
  if (!raw) {
    return fallback;
  }
  try {
    const value = JSON.parse(raw) as Partial<AppSettings>;
    return {
      newTaskPath: typeof value.newTaskPath === "string" ? value.newTaskPath : fallback.newTaskPath
    };
  } catch {
    return fallback;
  }
}

function saveSettings(settings: AppSettings) {
  localStorage.setItem(settingsKey, JSON.stringify(settings));
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

async function requestJSON<T>(url: string, method: string): Promise<T> {
  let response: Response;
  try {
    response = await fetch(url, { method });
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

function formatCount(count: number, singular: string): string {
  return `${count} ${singular}${count === 1 ? "" : "s"}`;
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

function eventLabel(event: RuntimeTaskEvent): string {
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
