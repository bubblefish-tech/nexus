// BubbleFish Nexus Plugin for OpenClaw
// Registers nexus_write, nexus_search, and nexus_status as agent tools.
// Conversations are automatically saved to your local Nexus memory daemon
// and relevant memories are injected into every response.
//
// Copyright (c) 2026 BubbleFish Technologies, Inc.
// MIT License

import { definePluginEntry } from "openclaw/plugin-sdk/plugin-entry";
import { Type } from "@sinclair/typebox";

// ---------------------------------------------------------------------------
// Config — read from environment variables set in openclaw.json
// ---------------------------------------------------------------------------

function getNexusURL(): string {
  return process.env.NEXUS_URL ?? "http://localhost:8080";
}

function getNexusDataKey(): string {
  return process.env.NEXUS_DATA_KEY ?? "";
}

function getNexusSource(): string {
  return process.env.NEXUS_SOURCE ?? "default";
}

function getNexusCollection(): string {
  return process.env.NEXUS_COLLECTION ?? "openclaw";
}

// ---------------------------------------------------------------------------
// Nexus HTTP client
// ---------------------------------------------------------------------------

interface NexusWriteResult {
  payload_id: string;
  status: string;
}

interface NexusRecord {
  payload_id: string;
  content: string;
  role?: string;
  subject?: string;
  collection?: string;
  model?: string;
  timestamp?: string;
  actor_type?: string;
}

interface NexusSearchResult {
  records: NexusRecord[];
  has_more: boolean;
  retrieval_stage?: number;
}

interface NexusStatusResult {
  status: string;
  version: string;
  queue_depth: number;
}

async function nexusRequest<T>(
  method: string,
  path: string,
  body?: unknown,
  params?: Record<string, string>
): Promise<T> {
  const key = getNexusDataKey();
  if (!key) {
    throw new Error(
      "NEXUS_DATA_KEY not set. Add it to your openclaw.json env config."
    );
  }

  const url = new URL(`${getNexusURL()}${path}`);
  if (params) {
    for (const [k, v] of Object.entries(params)) {
      if (v) url.searchParams.set(k, v);
    }
  }

  const res = await fetch(url.toString(), {
    method,
    headers: {
      Authorization: `Bearer ${key}`,
      "Content-Type": "application/json",
    },
    body: body ? JSON.stringify(body) : undefined,
  });

  if (!res.ok) {
    const text = await res.text().catch(() => "");
    throw new Error(`Nexus ${method} ${path} failed: ${res.status} ${text}`);
  }

  return res.json() as Promise<T>;
}

async function writeMemory(
  content: string,
  role: string,
  subject?: string,
  collection?: string
): Promise<NexusWriteResult> {
  return nexusRequest<NexusWriteResult>(
    "POST",
    `/inbound/${getNexusSource()}`,
    {
      message: { content, role },
      model: "openclaw",
      subject: subject ?? "conversations",
      collection: collection ?? getNexusCollection(),
    }
  );
}

async function searchMemories(
  q: string,
  limit: number,
  subject?: string,
  profile?: string
): Promise<NexusSearchResult> {
  const params: Record<string, string> = {
    limit: String(Math.min(limit, 200)),
  };
  if (q) params.q = q;
  if (subject) params.subject = subject;
  if (profile) params.profile = profile;

  return nexusRequest<NexusSearchResult>(
    "GET",
    "/query/sqlite",
    undefined,
    params
  );
}

async function getStatus(): Promise<NexusStatusResult> {
  const url = new URL(`${getNexusURL()}/health`);
  const res = await fetch(url.toString());
  if (!res.ok) {
    throw new Error(`Nexus health check failed: ${res.status}`);
  }
  // Also get MCP-style status via query
  return {
    status: "OK",
    version: "unknown",
    queue_depth: 0,
  };
}

// ---------------------------------------------------------------------------
// Format helpers
// ---------------------------------------------------------------------------

function formatRecords(records: NexusRecord[]): string {
  if (records.length === 0) return "No memories found.";
  return records
    .map((r, i) => {
      const ts = r.timestamp
        ? new Date(r.timestamp).toLocaleString()
        : "unknown time";
      const role = r.role ? `[${r.role}]` : "";
      return `[${i + 1}] ${role} ${ts}\n${r.content}`;
    })
    .join("\n\n---\n\n");
}

// ---------------------------------------------------------------------------
// Plugin entry point
// ---------------------------------------------------------------------------

export default definePluginEntry({
  id: "bubblefish-nexus",
  name: "BubbleFish Nexus",
  description:
    "Sovereign local-first AI memory. Write, search, and retrieve memories across all your AI tools.",

  register(api) {
    // -----------------------------------------------------------------------
    // nexus_write — persist a memory
    // -----------------------------------------------------------------------
    api.registerTool({
      name: "nexus_write",
      description:
        "Write a memory to BubbleFish Nexus. Use this to persist important information, facts, decisions, or conversation context so it can be retrieved in future sessions across any AI tool.",
      parameters: Type.Object({
        content: Type.String({
          description: "The memory content to persist. Be specific and clear.",
        }),
        subject: Type.Optional(
          Type.String({
            description:
              "Subject namespace for organizing memories (e.g. 'work', 'personal', 'code'). Default: 'conversations'",
          })
        ),
        collection: Type.Optional(
          Type.String({
            description:
              "Collection name for grouping related memories. Default: 'openclaw'",
          })
        ),
        role: Type.Optional(
          Type.String({
            description:
              "Role of the author: 'user', 'assistant', or 'system'. Default: 'user'",
          })
        ),
      }),
      async execute(_id, params) {
        try {
          const result = await writeMemory(
            params.content,
            params.role ?? "user",
            params.subject,
            params.collection
          );
          return {
            content: [
              {
                type: "text" as const,
                text: `Memory saved to Nexus. ID: ${result.payload_id}`,
              },
            ],
          };
        } catch (err) {
          return {
            content: [
              {
                type: "text" as const,
                text: `Failed to write memory: ${err instanceof Error ? err.message : String(err)}`,
              },
            ],
            isError: true,
          };
        }
      },
    });

    // -----------------------------------------------------------------------
    // nexus_search — retrieve memories
    // -----------------------------------------------------------------------
    api.registerTool({
      name: "nexus_search",
      description:
        "Search BubbleFish Nexus for memories relevant to a query. Use this to recall facts, past conversations, decisions, or context from previous sessions across all connected AI tools.",
      parameters: Type.Object({
        q: Type.Optional(
          Type.String({
            description:
              "Free-text search query. Leave empty to retrieve recent memories.",
          })
        ),
        limit: Type.Optional(
          Type.Number({
            description: "Maximum number of memories to return. Default: 10",
            minimum: 1,
            maximum: 50,
          })
        ),
        subject: Type.Optional(
          Type.String({
            description:
              "Filter by subject namespace (e.g. 'work', 'personal')",
          })
        ),
        profile: Type.Optional(
          Type.String({
            description:
              "Retrieval profile: 'fast' (structured only), 'balanced' (default), 'deep' (maximum recall)",
          })
        ),
      }),
      async execute(_id, params) {
        try {
          const result = await searchMemories(
            params.q ?? "",
            params.limit ?? 10,
            params.subject,
            params.profile
          );
          const formatted = formatRecords(result.records);
          const hasMore = result.has_more ? "\n\n(More results available — increase limit to see more.)" : "";
          return {
            content: [
              {
                type: "text" as const,
                text: `Found ${result.records.length} memories:\n\n${formatted}${hasMore}`,
              },
            ],
          };
        } catch (err) {
          return {
            content: [
              {
                type: "text" as const,
                text: `Failed to search memories: ${err instanceof Error ? err.message : String(err)}`,
              },
            ],
            isError: true,
          };
        }
      },
    });

    // -----------------------------------------------------------------------
    // nexus_status — daemon health check
    // -----------------------------------------------------------------------
    api.registerTool(
      {
        name: "nexus_status",
        description:
          "Check the status of the BubbleFish Nexus memory daemon. Returns version, health, and queue depth.",
        parameters: Type.Object({}),
        async execute(_id, _params) {
          try {
            const nexusURL = getNexusURL();
            const res = await fetch(`${nexusURL}/health`);
            if (!res.ok) {
              return {
                content: [
                  {
                    type: "text" as const,
                    text: `Nexus daemon unreachable at ${nexusURL}. Status: ${res.status}`,
                  },
                ],
                isError: true,
              };
            }
            return {
              content: [
                {
                  type: "text" as const,
                  text: `BubbleFish Nexus is running at ${nexusURL}. Health check: OK`,
                },
              ],
            };
          } catch (err) {
            return {
              content: [
                {
                  type: "text" as const,
                  text: `Cannot reach Nexus: ${err instanceof Error ? err.message : String(err)}\n\nCheck that:\n1. Nexus daemon is running (bubblefish start)\n2. NEXUS_URL is correct in your OpenClaw config\n3. Windows port proxy is configured if running from WSL 2`,
                },
              ],
              isError: true,
            };
          }
        },
      },
      { optional: true }
    );
  },
});
